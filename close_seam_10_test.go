package escp

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
)

// Close during the lookup of a MAC-only client waits for both windows of relocate (it asks
// twice when the first finds nobody), not "two seconds at most" in total as the doc of Close says.
func TestSeamCloseDuringLookupWaitsForBothWindows(t *testing.T) {
	var lookups atomic.Int32
	locator := func(ctx context.Context, _ MAC) (Info, error) {
		lookups.Add(1)
		select {
		case <-ctx.Done():
		case <-time.After(500 * time.Millisecond): // stands for Locate's window of 2 s
		}
		return Info{}, ErrNotFound
	}
	c, err := dial(context.Background(), net.DefaultResolver, Config{MAC: MAC{0, 0, 0x5e, 0, 0x53, 0x66}, Username: "admin", Password: "x", Timeout: time.Second,
		BroadcastAddr: escptest.Loopback(), SwitchPort: escptest.FreeUDPPort(t), HostPort: escptest.FreeUDPPort(t)}, withLoopback(), withLocator(locator))
	if err != nil {
		t.Fatal(err)
	}
	res := make(chan error, 1)
	go func() { _, err := c.Info(context.Background()); res <- err }()
	for lookups.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	_ = c.Close()
	took := time.Since(start)
	err = <-res
	t.Logf("Close took %v (one window is 500 ms); lookups=%d; Info returned %v (ErrClosed=%v)", took, lookups.Load(), err, errors.Is(err, ErrClosed))
	if took > 600*time.Millisecond {
		t.Errorf("Close waited for %v: more than the one window the doc promises", took)
	}
	if !errors.Is(err, ErrClosed) {
		t.Errorf("the call that Close cut short returned %v, not ErrClosed", err)
	}
}
