package escp

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
)

// A call whose context is already canceled sends nothing: not the key exchange, not the write.
func TestCanceledContextSendsNothing(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.SetLED(ctx, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if n := f.WriteCount(); n != 0 {
		t.Fatalf("the canceled call wrote %d time(s)", n)
	}
	if led, writes := f.LED(), f.WriteCount(); !led || writes != 0 {
		t.Fatalf("the switch was touched: led=%v writes=%d", led, writes)
	}
	// Not a datagram reached the switch. A later request, once answered, shows that everything
	// before it was processed, but not that both datagrams of its own probe were counted yet when
	// the first reply came back: so 1 or 2 are its own. A leaked call would have sent a discovery
	// probe first (2 datagrams more), which makes 3 or 4.
	if _, err := dialFake(t, f, "secret").Info(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := f.Received(); n > 2 {
		t.Fatalf("the canceled call sent datagrams: the switch has received %d, want only those of the later probe (at most 2)", n)
	}
}

// A method that sends two writes stops between them when the call is canceled in the gate of the
// second: the first was applied, the second is not sent.
func TestCancellationBetweenTwoWritesStopsTheSecond(t *testing.T) {
	f := escptest.Start(t)
	f.EnableModel(nil) // the pre-write read of the other limiter needs a switch that answers
	ctx, cancel := context.WithCancel(context.Background())
	var consulted int
	c := newBoundClient(t, Config{Host: "127.0.0.1", Username: "admin", Password: "secret", Timeout: 300 * time.Millisecond,
		SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback(),
		WriteGate: func(context.Context, Reader, Operation) error {
			if consulted++; consulted == 2 {
				cancel()
			}
			return nil
		}}, withLoopback())
	t.Cleanup(func() { c.Close() })
	err := c.SetBandwidth(ctx, PortBandwidth{Port: 2, Ingress: 64, Egress: 64})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if n := f.WriteCount(); n != 1 {
		t.Fatalf("want the ingress write only, the switch received %d", n)
	}
}

// A call that is waiting for a reply returns when it is canceled, not when the attempt times out.
func TestCancellationInterruptsTheWaitForAReply(t *testing.T) {
	f := escptest.Start(t)
	f.Mute()
	c := newBoundClient(t, Config{Host: "127.0.0.1", Username: "admin", Password: "secret", Timeout: 20 * time.Second,
		SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback()}, withLoopback())
	t.Cleanup(func() { c.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	start := time.Now()
	_, err := c.Info(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	// the attempt would have waited 20 s; anything far below that shows the wait was interrupted
	if took := time.Since(start); took > 10*time.Second {
		t.Fatalf("the canceled call kept waiting for %v", took)
	}
}

// The fragment exchange of the backup also sends nothing for a call that is already done.
func TestExchangeFragmentsSendsNothingForACanceledCall(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	if _, err := c.LED(context.Background()); err != nil { // opens the socket and the session
		t.Fatal(err)
	}
	before := f.Received()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.mu.Lock()
	_, err := c.exchangeFragments(ctx, wire.OpSet, []wire.TLV{{Type: wire.TLVConfigFile}}, wire.TLVConfigFile)
	c.mu.Unlock()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if _, err := c.LED(context.Background()); err != nil { // answered, so everything before it was processed
		t.Fatal(err)
	}
	if n := f.Received() - before; n != 1 {
		t.Fatalf("the canceled exchange sent datagrams: %d arrived since, want only the later read's 1", n)
	}
}

// blockingConn holds the first read until release is closed, which keeps the call that made it
// inside the client, holding its lock, for as long as a test needs.
type blockingConn struct {
	datagramConn
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *blockingConn) ReadFromUDP(buf []byte) (int, *net.UDPAddr, error) {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return b.datagramConn.ReadFromUDP(buf)
}

// A call that has to wait for another call on the same client gives up when its own context ends,
// without having sent anything, and the call it waited for is not disturbed.
//
// guarantee: Client
func TestACallThatWaitsForTheClientStopsWhenItsContextEnds(t *testing.T) {
	for name, call := range map[string]func(context.Context, *Client) error{
		"read":   func(ctx context.Context, c *Client) error { _, err := c.LED(ctx); return err },
		"info":   func(ctx context.Context, c *Client) error { _, err := c.Info(ctx); return err },
		"write":  func(ctx context.Context, c *Client) error { return c.SetLED(ctx, false) },
		"backup": func(ctx context.Context, c *Client) error { _, err := c.ConfigBackup(ctx); return err },
		"ports": func(ctx context.Context, c *Client) error {
			return c.SetPort(ctx, PortConfig{ID: 2, Enabled: true, Speed: SpeedAuto})
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := escptest.Start(t)
			f.EnableModel(nil)
			c := dialFake(t, f, "secret")
			if _, err := c.LED(context.Background()); err != nil { // opens the socket and the session
				t.Fatal(err)
			}
			hold := &blockingConn{datagramConn: c.conn, entered: make(chan struct{}), release: make(chan struct{})}
			c.conn = hold
			holder := make(chan error, 1)
			go func() { _, err := c.LED(context.Background()); holder <- err }()
			<-hold.entered // the holder is inside the client and has the lock
			written := f.WriteCount()

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			waiter := make(chan error, 1)
			go func() { waiter <- call(ctx, c) }()
			select {
			case err := <-waiter:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("the waiting call = %v, want context.DeadlineExceeded", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("the waiting call did not return while another call held the client")
			}
			close(hold.release)
			if err := <-holder; err != nil {
				t.Errorf("the call that held the client = %v, want it undisturbed", err)
			}
			if f.WriteCount() != written {
				t.Errorf("the waiting call wrote to the switch")
			}
			// a waiter that gave up must not leave the client locked
			if _, err := c.LED(context.Background()); err != nil {
				t.Errorf("the client is not usable after the abandoned wait: %v", err)
			}
		})
	}
}

// exchange sends nothing when the context ended before it started: the lock refuses a canceled
// call first, so this is the second line of defense, for a context that ends between the two.
func TestExchangeSendsNothingWhenTheContextEndedBeforeIt(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	if _, err := c.LED(context.Background()); err != nil { // opens the socket and the session
		t.Fatal(err)
	}
	before := f.Received()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.mu.Lock()
	_, err := c.exchange(ctx, wire.OpGet, nil, false, []*net.UDPAddr{c.dest})
	c.mu.Unlock()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("exchange = %v, want context.Canceled", err)
	}
	if _, err := c.LED(context.Background()); err != nil { // a later request shows what reached the switch before it
		t.Fatal(err)
	}
	if n := f.Received() - before; n > 2 { // its own request, and at most the datagram of a discovery
		t.Fatalf("the canceled exchange sent datagrams: %d reached the switch", n)
	}
}
