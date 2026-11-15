package escp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
)

// unsentWriteClock is the base of steppingClock: the deadline a drain read gets is a time before
// this one, and the deadline of a wait for a reply is a real time after it, which is how a test
// tells the two reads apart.
var unsentWriteClock = time.Unix(1<<20, 0)

// cutAtConn is a drainCutConn that ends the call during one chosen read: the first read, after the
// given number of datagrams were written through it, that is a drain read (the deadline is the
// stepping clock's) or a wait for a reply. Counting writes, not reads, keeps the place stable when
// a stale discovery reply is left in the socket.
type cutAtConn struct {
	*drainCutConn

	mu     sync.Mutex
	writes int  // datagrams written since the wrapper was put in
	drain  bool // the last read deadline was a drain's
	fired  bool

	at      int  // the number of datagrams written when the read runs
	atDrain bool // the read is a drain, not a wait for a reply
	during  func()
	wait    bool
}

func (s *cutAtConn) WriteToUDP(b []byte, addr *net.UDPAddr) (int, error) {
	s.mu.Lock()
	s.writes++
	s.mu.Unlock()
	return s.drainCutConn.WriteToUDP(b, addr)
}

func (s *cutAtConn) SetReadDeadline(t time.Time) error {
	s.mu.Lock()
	s.drain = t.Before(unsentWriteClock)
	s.mu.Unlock()
	return s.drainCutConn.SetReadDeadline(t)
}

func (s *cutAtConn) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	s.mu.Lock()
	fire := !s.fired && s.writes == s.at && s.drain == s.atDrain
	s.fired = s.fired || fire
	s.mu.Unlock()
	if fire {
		s.arm(s.during, s.wait)
	}
	return s.drainCutConn.ReadFromUDP(b)
}

func (s *cutAtConn) cut() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fired
}

// timesOutCtx is a context whose deadline passes when the test says so: Done closes, Err is
// context.DeadlineExceeded, as a context made by context.WithTimeout does when its timer fires.
type timesOutCtx struct {
	context.Context
	done chan struct{}
	once sync.Once
}

func (c *timesOutCtx) timeOut()                    { c.once.Do(func() { close(c.done) }) }
func (c *timesOutCtx) Done() <-chan struct{}       { return c.done }
func (c *timesOutCtx) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *timesOutCtx) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// unsentWindow is a place in a management write where the call can end. The write is Info (a
// discovery, two datagrams), the read that precedes a write that is not repeated (one datagram),
// then the write (one datagram per attempt); a call with no session starts with the key exchange
// between the first two.
type unsentWindow struct {
	name      string
	noSession bool // the client drops its session first, so the write starts with the key exchange
	at        int  // datagrams written when the read runs
	drain     bool // the read is a drain, not a wait for a reply
}

// The windows before the first datagram of the write; the write itself is the one after the last.
var windowsBeforeTheWrite = []unsentWindow{
	{name: "the drain before the read that precedes the write", at: 2, drain: true},
	{name: "the wait for the reply to the read that precedes the write", at: 3, drain: false},
	{name: "the drain before the write", at: 3, drain: true},
	{name: "the drain before the session key exchange", noSession: true, at: 2, drain: true},
}

// The windows after the write was sent and before it was answered.
var windowsAfterTheWrite = []unsentWindow{
	{name: "the wait for the reply to the write", at: 4, drain: false},
	{name: "the drain before the second attempt of the write", at: 4, drain: true},
}

// How a call ends during the read the test chose.
const (
	endByCancel   = "cancel"
	endByDeadline = "deadline"
	endByClose    = "close"
)

// managementWrites are the two writes that settle what they sent (SetIPConfig and EnableDHCP).
var managementWrites = []struct {
	name string
	do   func(context.Context, *Client) error
}{
	{"SetIPConfig", func(ctx context.Context, c *Client) error { return c.SetIPConfig(ctx, lostReplyWanted) }},
	{"EnableDHCP", func(ctx context.Context, c *Client) error { return c.EnableDHCP(ctx) }},
}

// cutCall runs do with a context that ends the way end says, during the read cut chooses, and fails
// the test when that read never came.
func cutCall(t *testing.T, c *Client, cut *cutAtConn, where, end string, do func(context.Context, *Client) error) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var closed sync.WaitGroup
	callCtx := ctx
	cut.wait = true // the context's end interrupts the read, and the read returns when it has
	switch end {
	case endByCancel:
		cut.during = cancel
	case endByClose:
		cut.during = func() { closed.Go(func() { _ = c.Close() }) } // Close waits for the call, which is in this read
	case endByDeadline:
		tc := &timesOutCtx{Context: ctx, done: make(chan struct{})}
		callCtx, cut.during = tc, tc.timeOut
	}
	err := do(callCtx, c)
	closed.Wait()
	if !cut.cut() {
		t.Fatalf("the call was never cut in %q: it returned %v", where, err)
	}
	return err
}

// runUnsentWindow runs one management write whose call ends in the window, the switch ignoring every
// write (so that a write that was sent is counted and not applied), and returns the number of
// writes the switch ignored, the number of lookups the client made, and the error.
func runUnsentWindow(t *testing.T, w unsentWindow, end string, do func(context.Context, *Client) error) (sent, lookups int, err error) {
	t.Helper()
	f := escptest.Start(t)
	f.Configure(func(f *escptest.Faults) { f.DropSets = true })
	c, looked := dialLeasing(t, f, withMoveWait(300*time.Millisecond))
	if _, err := c.LED(context.Background()); err != nil { // the session exists and the switch is known
		t.Fatal(err)
	}
	if w.noSession {
		c.mu.Lock()
		c.session = nil
		c.mu.Unlock()
	}
	cut := &cutAtConn{drainCutConn: &drainCutConn{datagramConn: c.conn}, at: w.at, atDrain: w.drain}
	c.conn = cut
	c.clock = steppingClock()

	err = cutCall(t, c, cut, w.name, end, do)
	return f.DroppedSets(), *looked, err
}

func wantedEnd(end string) error {
	switch end {
	case endByCancel:
		return context.Canceled
	case endByDeadline:
		return context.DeadlineExceeded
	default:
		return ErrClosed
	}
}

// A management write whose call ends before its first datagram was handed to the socket sent
// nothing, so nothing is settled: the call returns the reason it ended at once, and does not say
// that the switch may have moved (v0.10.1 probed the switch, then answered ErrMoveUnconfirmed).
//
// guarantee: ErrMoveUnconfirmed
// guarantee: Client.SetIPConfig
// guarantee: Client.EnableDHCP
func TestAManagementWriteEndedBeforeItsFirstDatagramIsNotSettled(t *testing.T) {
	for _, w := range windowsBeforeTheWrite {
		for _, end := range []string{endByCancel, endByDeadline, endByClose} {
			for _, write := range managementWrites {
				t.Run(w.name+"/"+end+"/"+write.name, func(t *testing.T) {
					sent, lookups, err := runUnsentWindow(t, w, end, write.do)
					if !errors.Is(err, wantedEnd(end)) {
						t.Errorf("error = %v, want %v", err, wantedEnd(end))
					}
					if errors.Is(err, ErrMoveUnconfirmed) || errors.Is(err, ErrNoReply) || errors.Is(err, ErrWriteUnconfirmed) {
						t.Errorf("error = %v: nothing was sent, so nothing is unconfirmed", err)
					}
					if sent != 0 {
						t.Errorf("the switch received the write %d time(s), want none", sent)
					}
					if lookups != 0 {
						t.Errorf("the client waited for a lease: %d lookup(s)", lookups)
					}
				})
			}
		}
	}
}

// A write that was sent and not answered stays settled, whichever way the call ends: a call that
// ends while it waits for the answer, or between its two attempts, still reports what a probe finds
// or ErrMoveUnconfirmed, and the second attempt of a call that ended is not sent.
//
// guarantee: ErrMoveUnconfirmed
func TestAManagementWriteSentAndNotAnsweredStaysSettledWhenTheCallEnds(t *testing.T) {
	for _, w := range windowsAfterTheWrite {
		for _, end := range []string{endByCancel, endByDeadline, endByClose} {
			for _, write := range managementWrites {
				t.Run(w.name+"/"+end+"/"+write.name, func(t *testing.T) {
					sent, _, err := runUnsentWindow(t, w, end, write.do)
					if !errors.Is(err, ErrMoveUnconfirmed) {
						t.Errorf("error = %v, want ErrMoveUnconfirmed: the write was sent", err)
					}
					if end == endByClose && !errors.Is(err, ErrClosed) {
						t.Errorf("error = %v, want it to say the client was closed too", err)
					}
					if sent != 1 {
						t.Errorf("the switch received the write %d time(s), want once", sent)
					}
				})
			}
		}
	}
}

// failWriteConn fails the nth datagram written through it (1-based), as a socket that cannot send
// does, and passes the others on.
type failWriteConn struct {
	datagramConn
	mu     sync.Mutex
	writes int
	fail   int
}

var errSocketWrite = errors.New("the socket cannot send")

func (s *failWriteConn) WriteToUDP(b []byte, addr *net.UDPAddr) (int, error) {
	s.mu.Lock()
	s.writes++
	failing := s.writes == s.fail
	s.mu.Unlock()
	if failing {
		return 0, errSocketWrite
	}
	return s.datagramConn.WriteToUDP(b, addr)
}

// A write that failed at the socket may have left in part, so it counts as sent and is settled: the
// mark "before the write" is for a refusal that came before the socket was asked to send.
//
// guarantee: ErrMoveUnconfirmed
func TestAManagementWriteThatFailedAtTheSocketIsStillSettled(t *testing.T) {
	for _, write := range managementWrites {
		t.Run(write.name, func(t *testing.T) {
			f := escptest.Start(t)
			c, _ := dialLeasing(t, f, withMoveWait(300*time.Millisecond))
			if _, err := c.LED(context.Background()); err != nil {
				t.Fatal(err)
			}
			// Info is two datagrams, the read before the write one, the write the fourth
			c.conn = &failWriteConn{datagramConn: c.conn, fail: 4}
			err := write.do(context.Background(), c)
			if !errors.Is(err, ErrMoveUnconfirmed) {
				t.Errorf("error = %v, want ErrMoveUnconfirmed: a probe settled the write", err)
			}
		})
	}
}

// The mark says only what it was given: it matches what it wraps, reads as it does, is set once,
// and is not found in an error that merely contains a marked one (a write that was sent first, then
// failed to re-key, is joined with the error of the re-key).
func TestTheMarkOfAnErrorBeforeTheWriteIsOnTheErrorItself(t *testing.T) {
	cause := errors.New("cause")
	marked := notWritten(cause)
	tests := []struct {
		name string
		got  bool
		want bool
	}{
		{"a marked error", isNotWritten(marked), true},
		{"a plain error", isNotWritten(cause), false},
		{"nil", isNotWritten(nil), false},
		{"an error that wraps a marked one", isNotWritten(fmt.Errorf("outer: %w", marked)), false},
		{"an error joined with a marked one", isNotWritten(errors.Join(cause, marked)), false},
		{"the mark removed", isNotWritten(unmarked(marked)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("isNotWritten = %v, want %v", tt.got, tt.want)
			}
		})
	}
	if !errors.Is(marked, cause) || marked.Error() != cause.Error() {
		t.Errorf("the mark must match and read as what it wraps: %v", marked)
	}
	if notWritten(marked) != marked { //nolint:errorlint // identity: marking twice returns the same error
		t.Error("an error marked twice is wrapped twice")
	}
	if notWritten(nil) != nil || unmarked(nil) != nil || unmarked(cause) != cause { //nolint:errorlint // identity: nothing to mark or remove
		t.Error("nil and a plain error must come back as they are")
	}
}

// A management write first opens a session and reads the switch with one authenticated request.
// When either is never answered the write was never sent, so the call ends with the plain ErrNoReply
// and probes nothing: the switch is known to be unchanged (v0.10.1 probed it anyway).
//
// guarantee: ErrMoveUnconfirmed
func TestAManagementWriteWhoseSessionOrReadBeforeItIsNeverAnsweredIsNotSettled(t *testing.T) {
	faults := []struct {
		name  string
		fault func(*escptest.Faults)
	}{
		{"the read before the write is lost", func(f *escptest.Faults) {
			f.OnGet = func(types []escptest.TLVType) ([]escptest.Setting, int32, bool) {
				return nil, 0, slices.Contains(types, escptest.TLVType(wire.TLVLED)) // only the read before the write is lost
			}
		}},
		{"the session key exchange is lost", func(f *escptest.Faults) { f.RSADrop = true }},
	}
	for _, fault := range faults {
		for _, write := range managementWrites {
			t.Run(fault.name+"/"+write.name, func(t *testing.T) {
				f := escptest.Start(t)
				f.Configure(func(f *escptest.Faults) {
					f.DropSets = true // a write that was sent would be counted
					fault.fault(f)
				})
				c, lookups := dialLeasing(t, f, withMoveWait(300*time.Millisecond))
				err := write.do(context.Background(), c)
				if !errors.Is(err, ErrNoReply) || errors.Is(err, ErrMoveUnconfirmed) || errors.Is(err, ErrWriteUnconfirmed) {
					t.Errorf("error = %v, want the plain ErrNoReply", err)
				}
				if strings.Contains(err.Error(), "probe") || *lookups != 0 {
					t.Errorf("the call settled a write that was never sent: %v (%d lookup(s))", err, *lookups)
				}
				if got := f.DroppedSets(); got != 0 {
					t.Errorf("the switch received the write %d time(s), want none", got)
				}
			})
		}
	}
}

// What an exchange knows after the send of an attempt: the request is written once any datagram of
// it was handed to the socket, in this attempt or an earlier one, and a refusal of a later attempt
// loses its mark then; the error that comes out never carries it.
func TestAnExchangeKnowsWhetherTheRequestWasWrittenAfterEachAttempt(t *testing.T) {
	refused := notWritten(context.Canceled)
	failed := errSocketWrite
	tests := []struct {
		name        string
		err         error
		before      bool
		wantWritten bool
		wantErr     error
	}{
		{name: "the first attempt is sent", err: nil, wantWritten: true},
		{name: "a later attempt is sent", err: nil, before: true, wantWritten: true},
		{name: "the first attempt is refused before the write", err: refused, wantWritten: false, wantErr: context.Canceled},
		{name: "a later attempt is refused after an earlier one was written", err: refused, before: true, wantWritten: true, wantErr: context.Canceled},
		{name: "the first attempt fails at the socket", err: failed, wantWritten: true, wantErr: errSocketWrite},
		{name: "a later attempt fails at the socket", err: failed, before: true, wantWritten: true, wantErr: errSocketWrite},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			written, err := writtenAfter(tt.err, tt.before)
			if written != tt.wantWritten {
				t.Errorf("written = %v, want %v", written, tt.wantWritten)
			}
			if !errors.Is(err, tt.wantErr) || isNotWritten(err) {
				t.Errorf("error = %v (marked: %v), want %v, never marked", err, isNotWritten(err), tt.wantErr)
			}
		})
	}
}

// exportedCalls is one call of each kind of exported method that sends a request: the reads that
// go through the TLV read, the discovery and the fragmented read, and the writes.
var exportedCalls = []struct {
	name string
	do   func(context.Context, *Client) error
}{
	{"LED", func(ctx context.Context, c *Client) error { _, err := c.LED(ctx); return err }},
	{"Info", func(ctx context.Context, c *Client) error { _, err := c.Info(ctx); return err }},
	{"Bandwidth", func(ctx context.Context, c *Client) error { _, err := c.Bandwidth(ctx); return err }},
	{"IGMP", func(ctx context.Context, c *Client) error { _, err := c.IGMP(ctx); return err }},
	{"LAGs", func(ctx context.Context, c *Client) error { _, err := c.LAGs(ctx); return err }},
	{"PortMirror", func(ctx context.Context, c *Client) error { _, err := c.PortMirror(ctx); return err }},
	{"QoS", func(ctx context.Context, c *Client) error { _, err := c.QoS(ctx); return err }},
	{"StormControl", func(ctx context.Context, c *Client) error { _, err := c.StormControl(ctx); return err }},
	{"LoopPrevention", func(ctx context.Context, c *Client) error { _, err := c.LoopPrevention(ctx); return err }},
	{"System", func(ctx context.Context, c *Client) error { _, err := c.System(ctx); return err }},
	{"MTUVLAN", func(ctx context.Context, c *Client) error { _, err := c.MTUVLAN(ctx); return err }},
	{"PortBasedVLAN", func(ctx context.Context, c *Client) error { _, err := c.PortBasedVLAN(ctx); return err }},
	{"Ports", func(ctx context.Context, c *Client) error { _, err := c.Ports(ctx); return err }},
	{"Dot1Q", func(ctx context.Context, c *Client) error { _, err := c.Dot1Q(ctx); return err }},
	{"PortStats", func(ctx context.Context, c *Client) error { _, err := c.PortStats(ctx); return err }},
	{"ConfigBackup", func(ctx context.Context, c *Client) error { _, err := c.ConfigBackup(ctx); return err }},
	{"SetLED", func(ctx context.Context, c *Client) error { return c.SetLED(ctx, true) }},
	{"SetLoopPrevention", func(ctx context.Context, c *Client) error { return c.SetLoopPrevention(ctx, true) }},
	{"SetHostname", func(ctx context.Context, c *Client) error { return c.SetHostname(ctx, "unsent") }},
	{"SetIPConfig", func(ctx context.Context, c *Client) error { return c.SetIPConfig(ctx, lostReplyWanted) }},
	{"EnableDHCP", func(ctx context.Context, c *Client) error { return c.EnableDHCP(ctx) }},
}

// A call that ends in the drain of its first request returns the context's error itself, as it did
// before the mark existed: callers compare it with ==, ask the error for Timeout, or look for it with
// errors.Is, and none of that may meet a type of this package. Where the call opens a session first
// the error is wrapped by the key exchange, and it matches the context's error and is not the mark.
func TestACallEndedInItsDrainReturnsTheContextsErrorWithoutTheMark(t *testing.T) {
	for _, call := range exportedCalls {
		for _, end := range []string{endByCancel, endByDeadline, endByClose} {
			for _, noSession := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/no session=%v", call.name, end, noSession), func(t *testing.T) {
					f := escptest.Start(t)
					c, _ := dialLeasing(t, f, withMoveWait(300*time.Millisecond))
					if _, err := c.LED(context.Background()); err != nil { // the session exists and the switch is known
						t.Fatal(err)
					}
					if noSession {
						c.mu.Lock()
						c.session = nil
						c.mu.Unlock()
					}
					cut := &cutAtConn{drainCutConn: &drainCutConn{datagramConn: c.conn}, at: 0, atDrain: true}
					c.conn, c.clock = cut, steppingClock()
					err := cutCall(t, c, cut, call.name, end, call.do)

					if marked, ok := errors.AsType[*notWrittenError](err); ok {
						t.Fatalf("the mark reached the caller: %T %v", marked, err)
					}
					want := wantedEnd(end)
					if !errors.Is(err, want) {
						t.Fatalf("error = %v, want %v", err, want)
					}
					if noSession && call.name != "Info" { // the key exchange wraps what it could not send; Info sends no key
						return
					}
					if err != want { //nolint:errorlint // the contract is the context's error itself
						t.Errorf("error = %T %v, want %v itself", err, err, want)
					}
					if end == endByDeadline {
						if netErr, ok := errors.AsType[net.Error](err); !ok || !netErr.Timeout() {
							t.Errorf("a call past its deadline must answer net.Error.Timeout: %T %v", err, err)
						}
					}
				})
			}
		}
	}
}

// A write that was sent in a first pass and went unanswered is repeated after a new session key
// (a write that cannot cut the management path is); a call that ends before the second pass writes
// anything still sent the first one, twice (one retry), and says so: sent is true across the passes.
func TestAWriteSentInTheFirstPassStaysSentWhenTheSecondPassEndsBeforeItsWrite(t *testing.T) {
	for _, end := range []string{endByCancel, endByDeadline, endByClose} {
		t.Run(end, func(t *testing.T) {
			f := escptest.Start(t)
			f.Configure(func(f *escptest.Faults) { f.DropSets = true })
			c, _ := dialLeasing(t, f)
			if _, err := c.LED(context.Background()); err != nil {
				t.Fatal(err)
			}
			// First pass: the write twice (one retry) without a reply, then the key exchange (one
			// datagram); the second pass ends in the drain before its write, which is the first drain
			// after three datagrams.
			cut := &cutAtConn{drainCutConn: &drainCutConn{datagramConn: c.conn}, at: 3, atDrain: true}
			c.conn, c.clock = cut, steppingClock()
			var sent bool
			err := cutCall(t, c, cut, "the second pass", end, func(ctx context.Context, c *Client) (err error) {
				sent, err = c.writeSystem(ctx, model.MethodSetHostname.Operation(), "unsent", false, lostReplyWanted)
				return err
			})
			if !sent {
				t.Errorf("sent = false, but the first pass wrote the request: %v", err)
			}
			if got := f.DroppedSets(); got != 2 {
				t.Errorf("the switch received the write %d time(s), want twice (the first pass)", got)
			}
		})
	}
}
