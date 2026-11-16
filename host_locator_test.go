package escp

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
)

// The lookup of a client is Locate on the interfaces the lister gives, except for a client whose
// Config.BroadcastAddr is one address of its own: that lookup goes to that address and never asks
// the lister (client.go, "&&" of hostLocator). The lister is the test's, so the answer does not
// depend on the interfaces of the machine it runs on. The reply port is held, so the lookup ends at
// the bind, after it has said how many interfaces and probes it would have used and before it
// sends anything.
func TestTheLookupOfAClientAsksTheListerUnlessItHasABroadcastAddressOfItsOwn(t *testing.T) {
	two := []netInterface{
		{Name: "en0", MAC: macOne, Broadcasts: []netip.Addr{netip.MustParseAddr("192.0.2.255")}},
		{Name: "en1", MAC: macTwo, Broadcasts: []netip.Addr{netip.MustParseAddr("198.51.100.255"), netip.MustParseAddr("203.0.113.255")}},
	}
	one := two[:1]
	tests := []struct {
		name           string
		broadcast      netip.Addr
		listed         []netInterface
		wantAsked      bool
		wantInterfaces int64
		wantProbes     int64
	}{
		{"the limited broadcast, two interfaces and three probes", limitedBroadcast, two, true, 2, 3},
		{"no broadcast address, two interfaces and three probes", netip.Addr{}, two, true, 2, 3},
		{"the limited broadcast, one interface and one probe", limitedBroadcast, one, true, 1, 1},
		{"no broadcast address, one interface and one probe", netip.Addr{}, one, true, 1, 1},
		{"an address of its own: the lister is not asked", escptest.Loopback(), two, false, 1, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			busy, err := net.ListenUDP("udp4", &net.UDPAddr{Port: escptest.FreeUDPPort(t)}) // does not share the port
			if err != nil {
				t.Fatal(err)
			}
			defer busy.Close()
			asked := false
			lister := func() ([]netInterface, error) { asked = true; return tc.listed, nil }
			log, rec := newLogRecorder()
			cfg := Config{BroadcastAddr: tc.broadcast, SwitchPort: escptest.FreeUDPPort(t), HostPort: localPort(t, busy)}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			_, _ = hostLocator(log, cfg, lister)(ctx, macOne)

			if asked != tc.wantAsked {
				t.Errorf("the lister was asked = %v, want %v", asked, tc.wantAsked)
			}
			lines := rec.named("discovery probes")
			if len(lines) != 1 {
				t.Fatalf("%d probe lines, want 1", len(lines))
			}
			if got := lines[0].attrs["interfaces"]; got != tc.wantInterfaces {
				t.Errorf("the lookup used %v interfaces, want %d", got, tc.wantInterfaces)
			}
			if got := lines[0].attrs["probes"]; got != tc.wantProbes {
				t.Errorf("the lookup sent %v probes, want %d", got, tc.wantProbes)
			}
		})
	}
}

// A host that lists no usable interface has nowhere to look, and what the lister could not list is
// the lookup's error.
func TestTheLookupOfAClientReportsWhatTheListerCannotGive(t *testing.T) {
	broken := errors.New("the interfaces cannot be listed")
	tests := []struct {
		name   string
		listed []netInterface
		err    error
		want   error
	}{
		{"no interface", nil, nil, ErrNoInterface},
		{"a lister that fails", nil, broken, broken},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lister := func() ([]netInterface, error) { return tc.listed, tc.err }
			_, err := hostLocator(nil, Config{SwitchPort: escptest.FreeUDPPort(t), HostPort: escptest.FreeUDPPort(t)}, lister)(context.Background(), macOne)
			if !errors.Is(err, tc.want) {
				t.Fatalf("lookup = %v, want %v", err, tc.want)
			}
		})
	}
}
