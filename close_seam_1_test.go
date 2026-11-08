package escp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
)

// Close during the lease wait of EnableDHCP. The doc of Close says it returns in moments.
func TestSeamCloseDuringLeaseWaitReturnsInMoments(t *testing.T) {
	f := escptest.Start(t)
	f.Configure(func(f *escptest.Faults) { f.OnSystemSet = escptest.SystemSetLeaves })
	var lookups atomic.Int32
	// like the production locator: a lookup with its own window, which knows nothing of Close
	locator := func(ctx context.Context, _ MAC) (Info, error) {
		lookups.Add(1)
		select {
		case <-ctx.Done():
		case <-time.After(200 * time.Millisecond):
		}
		return Info{}, ErrNotFound
	}
	c, _ := dialLeasing(t, f, withMoveWait(4*time.Second), withLocator(locator))
	res := make(chan error, 1)
	go func() { res <- c.EnableDHCP(context.Background()) }()
	for lookups.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	start := time.Now()
	_ = c.Close()
	took := time.Since(start)
	err := <-res
	t.Logf("Close took %v; EnableDHCP returned %v (is ErrClosed: %v)", took, err, errors.Is(err, ErrClosed))
	if took > time.Second {
		t.Errorf("Close did not return in moments: %v (documented: moments)", took)
	}
	if !errors.Is(err, ErrClosed) {
		t.Errorf("EnableDHCP after Close: want ErrClosed in the chain, got %v", err)
	}
}
