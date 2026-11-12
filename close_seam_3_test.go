package escp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
)

// Close while the management write waits for its reply: the settle probe runs on a closed client.
func TestSeamCloseMidManagementWriteSettlesOnAClosedClient(t *testing.T) {
	leaks, notClosed := 0, 0
	const rounds = 40
	for range rounds {
		f := escptest.Start(t)
		f.Configure(func(f *escptest.Faults) { f.DropSets = true })
		c := newBoundClient(t, Config{Host: "127.0.0.1", Username: "admin", Password: "secret", Timeout: 30 * time.Second,
			SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback()}, withLoopback())
		res := make(chan error, 1)
		go func() { res <- c.SetIPConfig(context.Background(), lostReplyWanted) }()
		awaitCondition(t, "the fake to drop the write", func() bool { return f.DroppedSets() > 0 })
		_ = c.Close()
		var err error
		select {
		case err = <-res:
		case <-time.After(20 * time.Second):
			t.Fatal("SetIPConfig did not end")
		}
		c.mu.Lock()
		reopened := c.conn != nil
		c.mu.Unlock()
		if reopened {
			leaks++
			_ = c.conn.Close()
		}
		if !errors.Is(err, ErrClosed) {
			notClosed++
			if notClosed == 1 {
				t.Logf("example error: %v", err)
			}
		}
	}
	t.Logf("of %d rounds: %d closed clients re-opened a socket after Close; %d SetIPConfig results are not ErrClosed", rounds, leaks, notClosed)
	if leaks > 0 || notClosed > 0 {
		t.Errorf("leaks=%d notErrClosed=%d", leaks, notClosed)
	}
}

// A closed client sends nothing and keeps its socket shut, whatever path asks: the two exchanges
// refuse it before they open or send.
func TestExchangesRefuseAClosedClient(t *testing.T) {
	f := escptest.Start(t)
	c := dialFast(t, f, "secret")
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.exchange(context.Background(), wire.OpDiscovery, nil, true, c.discoverTargets()); !errors.Is(err, ErrClosed) {
		t.Fatalf("exchange: got %v, want ErrClosed", err)
	}
	if _, err := c.exchangeFragments(context.Background(), wire.OpSet, nil, wire.TLVConfigFile); !errors.Is(err, ErrClosed) {
		t.Fatalf("exchangeFragments: got %v, want ErrClosed", err)
	}
	if c.conn != nil || f.Received() != 0 {
		t.Fatalf("a closed client opened its socket (%v) or sent %d datagram(s)", c.conn != nil, f.Received())
	}
}

// Close while EnableDHCP waits for the reply of its write: nothing is settled on a closed client,
// and the call says both that it was cut short and that the switch may have taken the change.
func TestCloseMidEnableDHCPWriteIsCutShort(t *testing.T) {
	f := escptest.Start(t)
	c, _ := dialLeasing(t, f, withMoveWait(5*time.Second))
	if _, err := c.Info(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.Configure(func(f *escptest.Faults) { f.DropSets = true })
	res := make(chan error, 1)
	go func() { res <- c.EnableDHCP(context.Background()) }()
	awaitCondition(t, "the fake to drop the write", func() bool { return f.DroppedSets() > 0 })
	closeWithin(t, c, 5*time.Second)
	if err := <-res; !errors.Is(err, ErrClosed) || !errors.Is(err, ErrMoveUnconfirmed) {
		t.Fatalf("got %v, want ErrClosed joined with ErrMoveUnconfirmed", err)
	}
}

// A caller that gives up while its management write waits for the reply: the write may still take
// effect, so the probe runs on a budget of its own, and a switch that still looks unchanged proves
// nothing yet (the write was not unanswered, the call just ended).
func TestSetIPConfigCanceledWhileItsWriteWaitsIsUnconfirmed(t *testing.T) {
	f := escptest.Start(t)
	c := newBoundClient(t, Config{Host: "127.0.0.1", Username: "admin", Password: "secret", Timeout: 5 * time.Second,
		SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback()}, withLoopback())
	t.Cleanup(func() { c.Close() })
	if _, err := c.Info(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.Configure(func(f *escptest.Faults) { f.DropSets = true })
	ctx, cancel := context.WithCancel(context.Background())
	res := make(chan error, 1)
	go func() { res <- c.SetIPConfig(ctx, lostReplyWanted) }()
	awaitCondition(t, "the fake to drop the write", func() bool { return f.DroppedSets() > 0 })
	cancel()
	err := <-res
	if !errors.Is(err, ErrMoveUnconfirmed) || !strings.Contains(err.Error(), "may not have taken effect yet") || errors.Is(err, ErrNoReply) {
		t.Fatalf("got %v, want ErrMoveUnconfirmed saying the write may not have taken effect yet", err)
	}
}
