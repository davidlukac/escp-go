package escp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
)

// Close during the wait for the new address of SetIPConfig.
func TestSeamCloseDuringMoveWaitReturnsInMoments(t *testing.T) {
	f := escptest.Start(t)
	c := dialFast(t, f, "secret", withMoveWait(4*time.Second))
	res := make(chan error, 1)
	go func() {
		res <- c.SetIPConfig(context.Background(), cfgOf("127.0.0.2", "255.255.255.0", "127.0.0.254"))
	}()
	for f.WriteCount() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond) // in awaitAddress by now
	start := time.Now()
	_ = c.Close()
	took := time.Since(start)
	err := <-res
	t.Logf("Close took %v; SetIPConfig returned %v (is ErrClosed: %v)", took, err, errors.Is(err, ErrClosed))
	if took > time.Second {
		t.Errorf("Close did not return in moments: %v", took)
	}
	if !errors.Is(err, ErrClosed) {
		t.Errorf("SetIPConfig after Close: want ErrClosed in the chain, got %v", err)
	}
}
