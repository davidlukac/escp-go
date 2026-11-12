package escp

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
)

// Close during the lookup of a MAC-only client does not wait for the windows of relocate (it asks
// twice when the first finds nobody): the lookup here has no window at all and ends only when the
// context it was given ends, so a Close that returns has ended the lookup, whatever the window of
// the real one is ("two seconds at most" in the doc of Close).
func TestSeamCloseDuringLookupDoesNotWaitForTheWindows(t *testing.T) {
	var lookups atomic.Int32
	inLookup := make(chan struct{})
	var once sync.Once
	locator := func(ctx context.Context, _ MAC) (Info, error) {
		lookups.Add(1)
		once.Do(func() { close(inLookup) })
		<-ctx.Done() // a lookup that knows nothing of its window: only its context ends it
		return Info{}, ErrNotFound
	}
	c, err := dial(context.Background(), net.DefaultResolver, Config{MAC: MAC{0, 0, 0x5e, 0, 0x53, 0x66}, Username: "admin", Password: "x", Timeout: time.Second,
		BroadcastAddr: escptest.Loopback(), SwitchPort: escptest.FreeUDPPort(t), HostPort: escptest.FreeUDPPort(t)}, withLoopback(), withLocator(locator))
	if err != nil {
		t.Fatal(err)
	}
	res := make(chan error, 1)
	go func() { _, err := c.Info(context.Background()); res <- err }()
	select {
	case <-inLookup: // the call is inside the lookup, which does not return by itself
	case err := <-res:
		t.Fatalf("Info ended with %v before it began the lookup", err)
	}
	closeWithin(t, c, callBound)
	err = <-res
	t.Logf("lookups=%d; Info returned %v (ErrClosed=%v)", lookups.Load(), err, errors.Is(err, ErrClosed))
	if !errors.Is(err, ErrClosed) {
		t.Errorf("the call that Close cut short returned %v, not ErrClosed", err)
	}
}
