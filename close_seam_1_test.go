package escp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
)

// Close during the lease wait of EnableDHCP. The doc of Close says it returns in moments.
func TestSeamCloseDuringLeaseWaitReturnsInMoments(t *testing.T) {
	f := escptest.Start(t)
	f.Configure(func(f *escptest.Faults) { f.OnSystemSet = escptest.SystemSetLeaves })
	inLookup := make(chan struct{})
	var once sync.Once
	// like the production locator, a lookup that knows nothing of Close: only the context it was given
	// ends it (there is no window to wait out, so a Close that returns has ended the lookup)
	locator := func(ctx context.Context, _ MAC) (Info, error) {
		once.Do(func() { close(inLookup) })
		<-ctx.Done()
		return Info{}, ErrNotFound
	}
	c, _ := dialLeasing(t, f, withMoveWait(4*time.Second), withLocator(locator))
	res := make(chan error, 1)
	go func() { res <- c.EnableDHCP(context.Background()) }()
	select {
	case <-inLookup: // EnableDHCP is inside the lookup, which does not return by itself
	case err := <-res:
		t.Fatalf("EnableDHCP ended with %v before it began the lookup", err)
	}
	start := time.Now()
	closeWithin(t, c, callBound)
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
