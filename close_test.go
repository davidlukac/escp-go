package escp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
)

// closeWithin runs Close and fails the test when it does not return in time.
func closeWithin(t *testing.T, c *Client, limit time.Duration) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- c.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(limit):
		t.Fatalf("Close did not return within %v: it waits for the call in flight", limit)
	}
}

// Close ends the call in flight and the calls queued behind it; it does not wait out a silent
// switch (a timeout of 30 seconds and two retries is minutes).
func TestCloseEndsTheCallInFlightAndTheCallsWaitingBehindIt(t *testing.T) {
	f := escptest.Start(t)
	c := newBoundClient(t, Config{Host: "127.0.0.1", Username: "admin", Password: "secret", Timeout: 30 * time.Second,
		SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback()}, withLoopback())
	t.Cleanup(func() { c.Close() })
	f.Mute()
	inFlight, queued := make(chan error, 1), make(chan error, 1)
	go func() { _, err := c.Ports(context.Background()); inFlight <- err }()
	for f.Received() == 0 { // the first call is waiting for the silent switch
		time.Sleep(time.Millisecond)
	}
	go func() { _, err := c.Ports(context.Background()); queued <- err }()
	time.Sleep(20 * time.Millisecond) // the second call is queued on the lock
	closeWithin(t, c, 5*time.Second)
	for name, ch := range map[string]chan error{"the call in flight": inFlight, "the queued call": queued} { // subtests: two results of one scenario, not cases
		select {
		case err := <-ch:
			if !errors.Is(err, ErrClosed) {
				t.Errorf("%s: got %v, want ErrClosed", name, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not end after Close", name)
		}
	}
}

// A write that Close cut short says that it was closed; the caller may not assume it was applied.
func TestAWriteCutShortByCloseReturnsErrClosed(t *testing.T) {
	f := escptest.Start(t)
	c := newBoundClient(t, Config{Host: "127.0.0.1", Username: "admin", Password: "secret", Timeout: 30 * time.Second,
		SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback()}, withLoopback())
	t.Cleanup(func() { c.Close() })
	if _, err := c.LED(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.Configure(func(f *escptest.Faults) { f.DropSets = true })
	res := make(chan error, 1)
	go func() { res <- c.SetLoopPrevention(context.Background(), true) }()
	for f.DroppedSets() == 0 {
		time.Sleep(time.Millisecond)
	}
	closeWithin(t, c, 5*time.Second)
	if err := <-res; !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v, want ErrClosed", err)
	}
}

// A Client that Dial did not make (the zero value) is a closed client: it returns ErrClosed
// instead of panicking. (A nil *Client is a nil pointer, which panics as every nil pointer does;
// only its Close is a no-op.)
func TestAClientNotMadeByDialIsClosed(t *testing.T) {
	ctx := context.Background()
	c := &Client{}
	for _, tt := range []struct {
		name string
		call func() error
	}{
		{"Info", func() error { _, err := c.Info(ctx); return err }},
		{"Ports", func() error { _, err := c.Ports(ctx); return err }},
		{"ConfigBackup", func() error { _, err := c.ConfigBackup(ctx); return err }},
		{"SetLED", func() error { return c.SetLED(ctx, true) }},
		{"SetHostname", func() error { return c.SetHostname(ctx, "sw") }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, ErrClosed) {
				t.Fatalf("got %v, want ErrClosed", err)
			}
		})
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	var none *Client
	if err := none.Close(); err != nil {
		t.Errorf("Close of a nil pointer: %v", err)
	}
}
