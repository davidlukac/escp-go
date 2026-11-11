package escp

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
)

// The tests below pin sentences of doc comments that no other test observed from outside (defect
// class DC-C01). Each carries a `// guarantee: <Name>` marker that `make guarantee` looks for.

// guarantee: Dial
// guarantee: Config.MAC
//
// Dial sends nothing to the switch and binds no socket, for an address and for a MAC alone: the local
// port stays free for whoever wants it until the first call.
func TestDialSendsNothingAndBindsNoSocket(t *testing.T) {
	tests := []struct {
		name string
		cfg  func(f *escptest.Switch) Config
	}{
		{"an address", func(f *escptest.Switch) Config {
			return Config{Host: "127.0.0.1", SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback()}
		}},
		{"a MAC and no address", func(f *escptest.Switch) Config {
			return Config{MAC: wantMAC, SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback()}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := escptest.Start(t)
			cfg := tc.cfg(f)
			cfg.HostPort = escptest.FreeUDPPort(t)
			c, err := dial(context.Background(), net.DefaultResolver, cfg, withLoopback())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { c.Close() })
			if c.conn != nil {
				t.Fatal("Dial opened the client's socket")
			}
			mine, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: cfg.HostPort})
			if err != nil {
				t.Fatalf("the local port is not free after Dial: %v", err)
			}
			if err := mine.Close(); err != nil {
				t.Fatal(err)
			}
			if got := f.Received(); got != 0 {
				t.Fatalf("Dial sent %d datagram(s) to the switch", got)
			}
		})
	}
}

// overlapConn counts the reads that wait for a reply at the same time: a serialized client has one.
type overlapConn struct {
	datagramConn
	active, most atomic.Int32
}

func (o *overlapConn) ReadFromUDP(buf []byte) (int, *net.UDPAddr, error) {
	n := o.active.Add(1)
	defer o.active.Add(-1)
	for {
		seen := o.most.Load()
		if n <= seen || o.most.CompareAndSwap(seen, n) {
			break
		}
	}
	return o.datagramConn.ReadFromUDP(buf)
}

// guarantee: Client
//
// A client is safe for concurrent use: calls from several goroutines (run with -race) all succeed, and
// only one request waits for the switch's reply at a time. The wait of a call for its turn ends with its
// context: TestACallThatWaitsForTheClientStopsWhenItsContextEnds.
func TestCallsFromSeveralGoroutinesAreAnsweredOneAtATime(t *testing.T) {
	f := escptest.Start(t)
	f.EnableModel(nil)
	c := dialFake(t, f, "secret")
	if _, err := c.LED(context.Background()); err != nil { // opens the socket and the session
		t.Fatal(err)
	}
	conn := &overlapConn{datagramConn: c.conn}
	c.conn = conn
	calls := []func(context.Context) error{
		func(ctx context.Context) error { _, err := c.LED(ctx); return err },
		func(ctx context.Context) error { _, err := c.Info(ctx); return err },
		func(ctx context.Context) error { _, err := c.Ports(ctx); return err },
		func(ctx context.Context) error { return c.SetLED(ctx, true) },
	}
	const rounds = 8
	errs := make(chan error, len(calls)*rounds)
	var wg sync.WaitGroup
	for _, call := range calls {
		for range rounds {
			wg.Go(func() { errs <- call(context.Background()) })
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("a call from one of several goroutines failed: %v", err)
		}
	}
	if most := conn.most.Load(); most != 1 {
		t.Errorf("%d requests waited for a reply at once, want one at a time", most)
	}
}

// guarantee: Config.Logger
//
// A client without a logger discards its messages and the library writes to no other logger: the
// default logger of the process hears nothing, not even from a retry or a refusal.
func TestAClientWithoutALoggerWritesToNoLogger(t *testing.T) {
	log, rec := newLogRecorder()
	previous := slog.Default()
	slog.SetDefault(log)
	t.Cleanup(func() { slog.SetDefault(previous) })
	f := escptest.Start(t)
	f.DropFirst(2) // a lost first attempt is a Warn line for a client with a logger
	c := dialFast(t, f, "secret")
	if err := c.SetLED(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	f.Configure(func(f *escptest.Faults) { f.DropSets = true })
	if err := c.SetLoopPrevention(context.Background(), true); !errors.Is(err, ErrWriteUnconfirmed) {
		t.Fatalf("got %v, want an unconfirmed write", err)
	}
	if got := rec.text(); got != "" {
		t.Fatalf("the default logger heard the client: %s", got)
	}
}

// guarantee: MaxTimeout
//
// The bound the documentation of MaxTimeout computes: a request holds a call for at most
// (MaxRetries+1) x MaxTimeout, 330 seconds, and Validate accepts exactly the values up to both limits.
func TestMaxTimeoutAndMaxRetriesBoundARequestBy330Seconds(t *testing.T) {
	if got := time.Duration(MaxRetries+1) * MaxTimeout; got != 330*time.Second {
		t.Errorf("(MaxRetries+1) x MaxTimeout = %v, the documentation says 330 seconds", got)
	}
	if err := (Config{Host: "192.0.2.11", Timeout: MaxTimeout, Retries: MaxRetries}).Validate(); err != nil {
		t.Errorf("the limits themselves must be accepted: %v", err)
	}
	if err := (Config{Host: "192.0.2.11", Timeout: MaxTimeout + 1}).Validate(); !errors.Is(err, ErrInvalid) {
		t.Errorf("a Timeout above the limit: %v", err)
	}
	if err := (Config{Host: "192.0.2.11", Retries: MaxRetries + 1}).Validate(); !errors.Is(err, ErrInvalid) {
		t.Errorf("Retries above the limit: %v", err)
	}
}

// guarantee: ErrWriteUnconfirmed
// guarantee: UnconfirmedWriteError
//
// A read that gets no reply matches ErrNoReply only: it is no unconfirmed write, there is nothing to
// have been applied. (The writes that are unconfirmed, and how often they were sent:
// TestAWriteTheSwitchNeverAnswersIsUnconfirmedAndSentAsOftenAsItsImpactAllows.)
func TestAReadThatGetsNoReplyIsNotAnUnconfirmedWrite(t *testing.T) {
	f := escptest.Start(t)
	c := dialFast(t, f, "secret")
	f.Mute()
	_, err := c.LED(context.Background())
	var unconfirmed *UnconfirmedWriteError
	if !errors.Is(err, ErrNoReply) || errors.Is(err, ErrWriteUnconfirmed) || errors.As(err, &unconfirmed) {
		t.Fatalf("a silent read = %v, want ErrNoReply only", err)
	}
}

// guarantee: ErrInvalid
// guarantee: model.ErrInvalid
//
// ErrInvalid is never returned for a reply the client cannot parse: the caller did nothing wrong. (A
// failure of the switch: TestFailuresOfTheSwitchAreNotErrInvalid.)
func TestAReplyTheClientCannotParseIsNotErrInvalid(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		reply []escptest.Setting
		call  func(c *Client) error
	}{
		{"a QoS mode of two bytes", []escptest.Setting{{Type: escptest.TLVType(wire.TLVQoSMode), Value: []byte{1, 2}}},
			func(c *Client) error { _, err := c.QoS(ctx); return err }},
		{"no QoS mode", nil, func(c *Client) error { _, err := c.QoS(ctx); return err }},
		{"no port mirror", nil, func(c *Client) error { _, err := c.PortMirror(ctx); return err }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := escptest.Start(t)
			c := dialFast(t, f, "secret")
			f.Configure(func(f *escptest.Faults) {
				f.OnGet = func([]escptest.TLVType) ([]escptest.Setting, int32, bool) { return tc.reply, 0, false }
			})
			err := tc.call(c)
			if err == nil {
				t.Fatal("a reply that cannot be read was accepted")
			}
			if errors.Is(err, ErrInvalid) {
				t.Fatalf("a reply the client cannot parse is ErrInvalid: %v", err)
			}
		})
	}
}

// guarantee: Client.SetLED
//
// SetLED is cosmetic: it is classed so for the gate, and it sends one SET that carries the LED setting
// and no other.
func TestSetLEDIsCosmeticAndChangesNothingButTheLED(t *testing.T) {
	f := escptest.Start(t)
	var seen []Operation
	c := dialFake(t, f, "secret")
	c.cfg.WriteGate = func(_ context.Context, _ Reader, op Operation) error { seen = append(seen, op); return nil }
	if err := c.SetLED(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0].Impact != model.ImpactCosmetic || !seen[0].Ports.IsEmpty() || !seen[0].Replaces.IsZero() || seen[0].AllPorts {
		t.Errorf("the gate was told %+v, want one cosmetic write that names no port and no group", seen)
	}
	if got, want := f.WriteCount(), 1; got != want {
		t.Errorf("%d writes reached the switch, want %d", got, want)
	}
	if got := f.LastSet(); len(got) != 1 || got[0].Type != escptest.TLVType(wire.TLVLED) {
		t.Errorf("the SET carries %+v, want only the LED setting", got)
	}
}

// guarantee: escptest.Switch.DroppedSets
//
// DroppedSets counts the writes the switch ignored, so that a test of a lost write can show it was
// sent: WriteCount stays zero for it, as for a write that never left the client.
func TestDroppedSetsCountsAWriteThatWasSentAndIgnored(t *testing.T) {
	f := escptest.Start(t)
	c := dialFast(t, f, "secret")
	if _, err := c.LED(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.Configure(func(f *escptest.Faults) { f.DropSets = true })
	if err := c.SetLED(context.Background(), false); !errors.Is(err, ErrWriteUnconfirmed) {
		t.Fatalf("got %v, want an unconfirmed write", err)
	}
	if f.DroppedSets() == 0 || f.WriteCount() != 0 {
		t.Errorf("a lost write: %d dropped, %d written; want some dropped and none written", f.DroppedSets(), f.WriteCount())
	}
	refused := dialFast(t, f, "secret")
	refused.cfg.WriteGate = func(context.Context, Reader, Operation) error { return errGateRefused }
	before := f.DroppedSets()
	if err := refused.SetLED(context.Background(), false); !errors.Is(err, errGateRefused) {
		t.Fatalf("got %v, want the gate's refusal", err)
	}
	if f.DroppedSets() != before {
		t.Errorf("a write that never left the client was counted as dropped")
	}
}

// guarantee: Client.SetStormControl
//
// SetStormControl refuses, before anything is sent, a traffic type without a rate, a rate that is no
// multiple of 64 kbps, one above model.MaxBandwidth, and a rate without a type; it accepts the lowest
// and the highest rate.
func TestSetStormControlRefusesWhatTheVendorUtilityRefuses(t *testing.T) {
	tests := []struct {
		name string
		cfg  StormConfig
		ok   bool
	}{
		{"a type without a rate", StormConfig{Port: 3, Broadcast: true}, false},
		{"a rate below the step", StormConfig{Port: 3, Broadcast: true, Rate: model.RateStep - 1}, false},
		{"a rate that is no multiple", StormConfig{Port: 3, Broadcast: true, Rate: model.RateStep + 1}, false},
		{"a rate above the maximum", StormConfig{Port: 3, Broadcast: true, Rate: model.MaxBandwidth + model.RateStep}, false},
		{"a rate without a type", StormConfig{Port: 3, Rate: model.RateStep}, false},
		{"the lowest rate", StormConfig{Port: 3, Multicast: true, Rate: model.RateStep}, true},
		{"the highest rate", StormConfig{Port: 3, UnknownUnicast: true, Rate: model.MaxBandwidth}, true},
		{"none selected, no rate: off", StormConfig{Port: 3}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, c := modelClient(t)
			err := c.SetStormControl(context.Background(), tc.cfg)
			if tc.ok {
				if err != nil || f.WriteCount() != 1 {
					t.Fatalf("got %v with %d writes, want one write", err, f.WriteCount())
				}
				return
			}
			if !errors.Is(err, ErrInvalid) || f.Received() != 0 {
				t.Fatalf("got %v with %d datagrams, want ErrInvalid and nothing sent", err, f.Received())
			}
		})
	}
}

// guarantee: model.Group
//
// Every write of the client names at most one group to replace: the documentation of model.Group says a
// Group has at most one field set, and a gate reads its members from the one that is.
func TestNoWriteReplacesMoreThanOneGroup(t *testing.T) {
	for _, name := range writeMethodNames() {
		t.Run(string(name), func(t *testing.T) {
			for i, op := range operationsOf(t, writeCalls[name]) {
				set := 0
				for _, id := range []bool{op.Replaces.LAG != 0, op.Replaces.PortVLAN != 0, op.Replaces.Dot1QVLAN != 0} {
					if id {
						set++
					}
				}
				if set > 1 {
					t.Errorf("consultation %d names %+v: more than one group", i+1, op.Replaces)
				}
			}
		})
	}
}
