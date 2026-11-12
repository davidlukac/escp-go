package escp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
)

// The check-then-write guards of SetBandwidth and SetStormControl are serialized like the other
// read-modify-write calls: while one has passed its check and waits for its write, the other waits for
// its turn, and then its own check sees the setting of the first and refuses. Before, both passed,
// both wrote, and the switch turned one setting off without a word (what each guard exists to prevent).
func TestStormControlAndAnIngressLimitOnOnePortDoNotBothGetThrough(t *testing.T) {
	f := escptest.Start(t)
	f.EnableModel(nil)
	hold, release := make(chan struct{}), make(chan struct{})
	c := newBoundClient(t, Config{Host: "127.0.0.1", Username: "admin", Password: "secret", Timeout: time.Second,
		SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback(),
		WriteGate: func(ctx context.Context, _ Reader, op Operation) error {
			if op.Name == MethodSetStormControl {
				close(hold) // its guard (no ingress limit on the port) has passed
				<-release
			}
			return nil
		}}, withLoopback())
	t.Cleanup(func() { c.Close() })
	storm := make(chan error, 1)
	go func() {
		storm <- c.SetStormControl(context.Background(), StormConfig{Port: 2, Broadcast: true, Rate: 64})
	}()
	<-hold
	bw := make(chan error, 1)
	go func() { bw <- c.SetBandwidth(context.Background(), PortBandwidth{Port: 2, Ingress: 640}) }()
	if err, finished := awaitSecond(bw); finished { // it must queue behind the other call, not end
		t.Fatalf("SetBandwidth did not wait for the storm control write that is in progress: %v", err)
	}
	close(release)
	if err := <-storm; err != nil {
		t.Fatalf("the first call: %v", err)
	}
	if err := <-bw; !errors.Is(err, ErrInvalid) {
		t.Fatalf("the second call must see the storm control of the first and be refused, got %v", err)
	}
}

// A call that waits for its turn behind another gives up when its context ends, sending nothing.
func TestTheLimitWritesGiveUpWhenTheirContextEndsBeforeTheirTurn(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tt := range []struct {
		name  string
		write func() error
	}{
		{"SetBandwidth", func() error { return c.SetBandwidth(ctx, PortBandwidth{Port: 2, Ingress: 640}) }},
		{"SetStormControl", func() error { return c.SetStormControl(ctx, StormConfig{Port: 2, Broadcast: true, Rate: 64}) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.write(); !errors.Is(err, context.Canceled) {
				t.Fatalf("got %v, want the cancellation", err)
			}
			if f.Received() != 0 {
				t.Fatalf("%d datagram(s) were sent for a call that was canceled before its turn", f.Received())
			}
		})
	}
}
