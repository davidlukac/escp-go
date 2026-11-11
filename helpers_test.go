package escp

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/udp"
)

// testMovePoll is the pause between probes while a test waits for a moved switch or a lease: the
// production pause is half a second, which only makes tests slow.
const testMovePoll = 20 * time.Millisecond

// newBoundClient dials a client on a free local port and binds its socket
// immediately, retrying with another port if something grabbed the first one
// in the meantime. Once it returns, the port is held by the client, so two
// tests can never end up on the same port.
func newBoundClient(tb testing.TB, cfg Config, opts ...dialOption) *Client {
	tb.Helper()
	var lastErr error
	for range 20 {
		cfg.HostPort = escptest.FreeUDPPort(tb)
		c, err := dial(context.Background(), net.DefaultResolver, cfg, append([]dialOption{withMovePoll(testMovePoll)}, opts...)...)
		if err != nil {
			tb.Fatal(err)
		}
		c.mu.Lock()
		lastErr = c.open(context.Background())
		c.mu.Unlock()
		if lastErr == nil {
			return c
		}
	}
	tb.Fatalf("no free UDP port after 20 attempts: %v", lastErr)
	return nil
}

func dialFake(t *testing.T, f *escptest.Switch, pass Password, opts ...dialOption) *Client {
	t.Helper()
	c := newBoundClient(t, Config{Host: "127.0.0.1", Username: "admin", Password: pass, Timeout: 300 * time.Millisecond,
		SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback()}, append([]dialOption{withLoopback()}, opts...)...)
	t.Cleanup(func() { c.Close() })
	return c
}

// dialFast is dialFake with short timeouts and a single retry, for tests that
// deliberately make the switch stay silent.
func dialFast(t *testing.T, f *escptest.Switch, pass Password, opts ...dialOption) *Client {
	t.Helper()
	c := newBoundClient(t, Config{Host: "127.0.0.1", Username: "admin", Password: pass, Timeout: 80 * time.Millisecond, Retries: 1,
		SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback()}, append([]dialOption{withLoopback()}, opts...)...)
	t.Cleanup(func() { c.Close() })
	return c
}

// localPort is the UDP port a socket is bound to.
func localPort(tb testing.TB, c interface{ LocalAddr() net.Addr }) int {
	tb.Helper()
	ap, err := udp.AddrPort(c.LocalAddr())
	if err != nil {
		tb.Fatal(err)
	}
	return int(ap.Port())
}
