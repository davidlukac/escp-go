package escp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
)

// Close lands between the gate and the lock of a management write: nothing was sent, but
// the call settles it anyway, on a closed client.
func TestSeamSetIPConfigOnAClientClosedBeforeTheWriteTouchesTheSwitchAgain(t *testing.T) {
	f := escptest.Start(t)
	var c *Client
	var atClose int
	c = newBoundClient(t, Config{Host: "127.0.0.1", Username: "admin", Password: "secret", Timeout: 200 * time.Millisecond, Retries: 1,
		SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback(),
		WriteGate: func(ctx context.Context, _ Reader, op Operation) error {
			atClose = f.Received()
			_ = c.Close()
			return nil
		}}, withLoopback())
	if _, err := c.Info(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := c.SetIPConfig(context.Background(), lostReplyWanted)
	after := f.Received()
	c.mu.Lock()
	reopened := c.conn != nil
	c.mu.Unlock()
	t.Logf("err=%v ; ErrClosed=%v ; datagrams sent to the switch after Close: %d ; socket re-opened after Close: %v ; writes applied: %d",
		err, errors.Is(err, ErrClosed), after-atClose, reopened, f.WriteCount())
	if reopened {
		_ = c.conn.Close()
	}
	if after != atClose || reopened || !errors.Is(err, ErrClosed) {
		t.Errorf("a closed client sent %d datagram(s), re-opened its socket=%v, returned %v", after-atClose, reopened, err)
	}
}
