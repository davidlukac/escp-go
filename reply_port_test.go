package escp

import (
	"testing"

	"github.com/davidlukac/escp-go/escptest"
)

// The helpers that dial the fake give each client a reply port of its own, so that no test of this package
// shares the real port 29809 with another ESCP client on the host (#237, DC-C06). The hostport check of
// `make lint` keeps a test from leaving the port out of a Config literal; these pin the helpers it trusts.
func TestTheDialHelpersGiveTheClientItsOwnReplyPort(t *testing.T) {
	own := escptest.FreeUDPPort(t)
	for _, tc := range []struct {
		name     string
		hostPort int  // what the config names
		named    bool // the client keeps it
	}{
		{"claimed picks a port when the config names none", 0, false},
		{"claimed keeps the port the config names", own, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := claimed(t, Config{Host: "127.0.0.1", SwitchPort: escptest.FreeUDPPort(t), HostPort: tc.hostPort})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { c.Close() })
			got := c.cfg.HostPort
			if tc.named && got != own || !tc.named && (got == 0 || got == defaultHostPort) {
				t.Fatalf("the client's reply port is %d", got)
			}
		})
	}
	if got := dialFake(t, escptest.Start(t), "secret").cfg.HostPort; got == 0 || got == defaultHostPort {
		t.Fatalf("the fake's dial helper left the client on reply port %d", got)
	}
}

// Two clients that claim the same switch through the helper, one after the other, are given two ports.
func TestClaimedGivesEachClientAPortOfItsOwn(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", SwitchPort: escptest.FreeUDPPort(t)}
	first, err := claimed(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { first.Close() })
	first.Close()
	second, err := claimed(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { second.Close() })
	if first.cfg.HostPort == second.cfg.HostPort {
		t.Fatalf("both clients got reply port %d", first.cfg.HostPort)
	}
}
