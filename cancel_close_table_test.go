package escp

import (
	"context"
	"errors"
	"net"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
)

// callBound is how long a call, or a Close, may take in these tests before the test says it did not
// end. It bounds a wait that must not happen; nothing is synchronized with it, and the fake's
// timeout of 30 seconds is what the calls would wait without the context or Close.
const callBound = 5 * time.Second

// closeRow is one method of Client that takes a context: a call of it with arguments that are valid,
// so that the only reason for it to end early is the context or Close.
type closeRow struct {
	name string
	call func(context.Context, *Client) error
}

// closeRows is the table: every reader here, and every writer of writeCalls (the table of the write
// gate, which already holds valid arguments for each). TestEveryMethodEndsWithItsContextAndClose
// finds the methods by reflection and fails for a method that has no row, so a new method that takes
// a context cannot skip the checks below unnoticed.
func closeRows() []closeRow {
	rows := []closeRow{
		{"Bandwidth", func(ctx context.Context, c *Client) error { _, err := c.Bandwidth(ctx); return err }},
		{"ConfigBackup", func(ctx context.Context, c *Client) error { _, err := c.ConfigBackup(ctx); return err }},
		{"Dot1Q", func(ctx context.Context, c *Client) error { _, err := c.Dot1Q(ctx); return err }},
		{"IGMP", func(ctx context.Context, c *Client) error { _, err := c.IGMP(ctx); return err }},
		{"Info", func(ctx context.Context, c *Client) error { _, err := c.Info(ctx); return err }},
		{"LAGs", func(ctx context.Context, c *Client) error { _, err := c.LAGs(ctx); return err }},
		{"LED", func(ctx context.Context, c *Client) error { _, err := c.LED(ctx); return err }},
		{"LoopPrevention", func(ctx context.Context, c *Client) error { _, err := c.LoopPrevention(ctx); return err }},
		{"MTUVLAN", func(ctx context.Context, c *Client) error { _, err := c.MTUVLAN(ctx); return err }},
		{"PortBasedVLAN", func(ctx context.Context, c *Client) error { _, err := c.PortBasedVLAN(ctx); return err }},
		{"PortMirror", func(ctx context.Context, c *Client) error { _, err := c.PortMirror(ctx); return err }},
		{"PortStats", func(ctx context.Context, c *Client) error { _, err := c.PortStats(ctx); return err }},
		{"Ports", func(ctx context.Context, c *Client) error { _, err := c.Ports(ctx); return err }},
		{"QoS", func(ctx context.Context, c *Client) error { _, err := c.QoS(ctx); return err }},
		{"StormControl", func(ctx context.Context, c *Client) error { _, err := c.StormControl(ctx); return err }},
		{"System", func(ctx context.Context, c *Client) error { _, err := c.System(ctx); return err }},
		{"VLANMode", func(ctx context.Context, c *Client) error { _, err := c.VLANMode(ctx); return err }},
	}
	for method, call := range writeCalls {
		rows = append(rows, closeRow{string(method), call})
	}
	slices.SortFunc(rows, func(a, b closeRow) int {
		switch {
		case a.name < b.name:
			return -1
		case a.name > b.name:
			return 1
		}
		return 0
	})
	return rows
}

// contextMethods names the exported methods of typ that take a context.Context, in order.
func contextMethods(typ reflect.Type) []string {
	ctxType := reflect.TypeFor[context.Context]()
	var names []string
	for i := range typ.NumMethod() {
		m := typ.Method(i)
		for j := 1; j < m.Type.NumIn(); j++ { // 0 is the receiver
			if m.Type.In(j) == ctxType {
				names = append(names, m.Name)
				break
			}
		}
	}
	return names
}

// rowDifference says which of the methods have no row and which rows name no method.
func rowDifference(methods []string, rows []closeRow) (missing, stale []string) {
	rowed := map[string]bool{}
	for _, r := range rows {
		rowed[r.name] = true
		if !slices.Contains(methods, r.name) {
			stale = append(stale, r.name)
		}
	}
	for _, m := range methods {
		if !rowed[m] {
			missing = append(missing, m)
		}
	}
	return missing, stale
}

// signalingConn is the client's socket with a view of it: it says when the client waits for a reply
// to a request it has sent, and counts the datagrams the client sends.
type signalingConn struct {
	datagramConn
	waiting chan struct{}
	once    sync.Once
	sent    atomic.Int32
}

func newSignalingConn(conn datagramConn) *signalingConn {
	return &signalingConn{datagramConn: conn, waiting: make(chan struct{})}
}

func (s *signalingConn) ReadFromUDP(buf []byte) (int, *net.UDPAddr, error) {
	if s.sent.Load() > 0 { // a read before the first send is the drain of stale replies, not the wait for this one
		s.once.Do(func() { close(s.waiting) })
	}
	return s.datagramConn.ReadFromUDP(buf)
}

func (s *signalingConn) WriteToUDP(b []byte, addr *net.UDPAddr) (int, error) {
	s.sent.Add(1)
	return s.datagramConn.WriteToUDP(b, addr)
}

// usedClient is a client of the fake that has used its switch once, so its socket and its session
// are open: the state in which a caller's context or Close has something to end.
func usedClient(t *testing.T, f *escptest.Switch, timeout time.Duration) *Client {
	t.Helper()
	c := newBoundClient(t, Config{Host: "127.0.0.1", Username: "admin", Password: "secret", Timeout: timeout,
		SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback()}, withLoopback())
	t.Cleanup(func() { c.Close() })
	if _, err := c.LED(context.Background()); err != nil {
		t.Fatal(err)
	}
	return c
}

// resultWithin runs call and waits for it, up to callBound, for the case that it does not end.
func resultWithin(t *testing.T, call func() error) error {
	t.Helper()
	res := make(chan error, 1)
	go func() { res <- call() }()
	select {
	case err := <-res:
		return err
	case <-time.After(callBound):
		t.Fatalf("the call did not end within %v", callBound)
		return nil
	}
}

// reachedOnly shows that the fake received nothing since before except the one datagram of probe's
// next read. The probe has used the switch already, so its read is one datagram; the fake handles the
// datagrams of every client on one socket in the order they arrived, so once the probe is answered
// every datagram sent before it has been counted.
func reachedOnly(t *testing.T, f *escptest.Switch, before int, probe *Client) {
	t.Helper()
	if _, err := probe.LED(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.Received() - before; got != 1 {
		t.Fatalf("the fake received %d datagram(s) since the call began, want only the 1 of the probe: the call sent something", got)
	}
}

// A call whose context is already canceled returns the context's error and sends nothing.
func canceledCallSendsNothing(t *testing.T, call func(context.Context, *Client) error) {
	f := escptest.Start(t)
	f.EnableModel(nil)
	c, probe := usedClient(t, f, 300*time.Millisecond), usedClient(t, f, 300*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := f.Received()
	if err := resultWithin(t, func() error { return call(ctx, c) }); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	reachedOnly(t, f, before, probe)
}

// A call cut off by Close while the switch stays silent returns ErrClosed, and sends nothing more:
// not a retry, and not on a socket the client opened again.
func closeEndsACallWaitingForASilentSwitch(t *testing.T, call func(context.Context, *Client) error) {
	f := escptest.Start(t)
	f.EnableModel(nil)
	c := usedClient(t, f, 30*time.Second) // what the call would wait for without Close: minutes
	conn := newSignalingConn(c.conn)
	c.conn = conn
	f.Mute()
	res := make(chan error, 1)
	go func() { res <- call(context.Background(), c) }()
	select {
	case <-conn.waiting: // the call sent its request and waits for a reply that never comes
	case err := <-res:
		t.Fatalf("the call ended with %v before it waited for the silent switch: the row's arguments must reach the switch", err)
	case <-time.After(callBound):
		t.Fatalf("the call neither waited for the switch nor ended within %v", callBound)
	}
	sentWhenClosing := conn.sent.Load()
	closeWithin(t, c, callBound)
	select {
	case err := <-res:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("got %v, want ErrClosed", err)
		}
	case <-time.After(callBound):
		t.Fatalf("the call was still waiting %v after Close", callBound)
	}
	if sent := conn.sent.Load(); sent != sentWhenClosing {
		t.Errorf("the client sent %d datagram(s) after Close began", sent-sentWhenClosing)
	}
	c.mu.Lock()
	reopened := c.conn != nil
	c.mu.Unlock()
	if reopened {
		t.Error("the client holds a socket after Close: a closed client opened one again")
	}
}

// A call after Close returns ErrClosed and sends nothing.
func callAfterCloseSendsNothing(t *testing.T, call func(context.Context, *Client) error) {
	f := escptest.Start(t)
	f.EnableModel(nil)
	c, probe := usedClient(t, f, 300*time.Millisecond), usedClient(t, f, 300*time.Millisecond)
	closeWithin(t, c, callBound)
	before := f.Received()
	if err := resultWithin(t, func() error { return call(context.Background(), c) }); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v, want ErrClosed", err)
	}
	reachedOnly(t, f, before, probe)
}

// Every exported method of Client that takes a context is in the table of closeRows, and each ends
// with the caller's context and with Close (DC-L01): an already canceled context returns its error
// and sends nothing; Close ends a call that waits for a silent switch and says ErrClosed; a call
// after Close says ErrClosed and sends nothing. The table is found by reflection, so adding a method
// that takes a context without a row fails here, and a row for a method that is gone fails too.
func TestEveryMethodEndsWithItsContextAndClose(t *testing.T) {
	rows := closeRows()
	methods := contextMethods(reflect.TypeFor[*Client]())
	if len(methods) < 30 { // a floor against a broken scan, not a count of the methods
		t.Fatalf("found only %d methods that take a context by reflection; the scan is broken", len(methods))
	}
	missing, stale := rowDifference(methods, rows)
	for _, name := range missing {
		t.Errorf("%s takes a context but has no row in closeRows: add one, with arguments that are valid, so the cancel and Close cases below run for it", name)
	}
	for _, name := range stale {
		t.Errorf("closeRows has a row for %s, which is not a method of Client that takes a context: remove it", name)
	}
	cases := []struct {
		name string
		run  func(*testing.T, func(context.Context, *Client) error)
	}{
		{"a canceled context returns its error and sends nothing", canceledCallSendsNothing},
		{"Close ends a call that waits for a silent switch", closeEndsACallWaitingForASilentSwitch},
		{"a call after Close returns ErrClosed and sends nothing", callAfterCloseSendsNothing},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel() // each case has a fake switch and clients of its own
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) { tc.run(t, row.call) })
			}
		})
	}
}

// The completeness check finds the methods that take a context and tells a missing row from a stale one.
func TestTheMethodScanFindsTheMethodsThatTakeAContext(t *testing.T) {
	_ = scanned.hidden // the unexported method is there to be left out
	if got, want := contextMethods(reflect.TypeFor[scanned]()), []string{"Both", "First", "Later"}; !slices.Equal(got, want) {
		t.Fatalf("found %v, want %v", got, want)
	}
	for _, tc := range []struct {
		name                 string
		methods              []string
		rows                 []closeRow
		wantMissing, wantOld []string
	}{
		{"complete", []string{"A", "B"}, []closeRow{{name: "A"}, {name: "B"}}, nil, nil},
		{"a method without a row", []string{"A", "B"}, []closeRow{{name: "A"}}, []string{"B"}, nil},
		{"a row without a method", []string{"A"}, []closeRow{{name: "A"}, {name: "Gone"}}, nil, []string{"Gone"}},
		{"both", []string{"A", "B"}, []closeRow{{name: "B"}, {name: "Gone"}}, []string{"A"}, []string{"Gone"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			missing, stale := rowDifference(tc.methods, tc.rows)
			if !slices.Equal(missing, tc.wantMissing) || !slices.Equal(stale, tc.wantOld) {
				t.Fatalf("missing %v, stale %v; want %v, %v", missing, stale, tc.wantMissing, tc.wantOld)
			}
		})
	}
}

// scanned is a type for the method scan: three methods take a context (the context is not always the
// first argument), one does not, and one is not exported.
type scanned struct{}

func (scanned) First(context.Context) error           { return nil }
func (scanned) Later(int, context.Context) error      { return nil }
func (scanned) Both(context.Context, context.Context) {}
func (scanned) None(int) error                        { return nil }
func (scanned) hidden(context.Context) error          { return nil }
