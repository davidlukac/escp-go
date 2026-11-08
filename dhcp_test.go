package escp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
)

// dialLeasing is a client for the fake that pins its MAC and looks the switch up by asking the fake
// itself (the fake cannot be found by a broadcast on this host). The returned counter is the number
// of lookups.
func dialLeasing(t *testing.T, f *escptest.Switch, opts ...dialOption) (*Client, *int) {
	t.Helper()
	lookups := new(int)
	askTheFake := func(c *Client) { // the lookup needs the client, which does not exist yet
		c.locator = func(ctx context.Context, _ MAC) (Info, error) { *lookups++; return c.discoverOnce(ctx) }
	}
	c := newBoundClient(t, Config{Host: "127.0.0.1", MAC: escptest.DefaultMAC(), Username: "admin", Password: "secret",
		Timeout: 80 * time.Millisecond, Retries: 1, SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback()},
		append([]dialOption{withLoopback(), askTheFake}, opts...)...)
	t.Cleanup(func() { c.Close() })
	return c, lookups
}

func TestEnableDHCPAsksForALeaseAndFollowsIt(t *testing.T) {
	f := escptest.Start(t)
	c, _ := dialLeasing(t, f)
	if err := c.EnableDHCP(context.Background()); err != nil {
		t.Fatal(err)
	}
	sets := f.AllSets()
	if len(sets) != 1 {
		t.Fatalf("want exactly one write, got %d", len(sets))
	}
	// The request carries the description and the current static address as read, and the flag on.
	want := []wire.TLV{
		{Type: wire.TLVHostname, Value: []byte("TL-SG108E-fake\x00")}, {Type: wire.TLVDHCP, Value: []byte{1}},
		{Type: wire.TLVIP, Value: []byte{127, 0, 0, 1}}, {Type: wire.TLVNetmask, Value: []byte{255, 255, 255, 0}}, {Type: wire.TLVGateway, Value: []byte{127, 0, 0, 254}},
	}
	if len(sets[0]) != len(want) {
		t.Fatalf("SET carries %d TLVs, want %d: %+v", len(sets[0]), len(want), sets[0])
	}
	for i, w := range want {
		t.Run(fmt.Sprintf("TLV %d", i), func(t *testing.T) {
			if sets[0][i].Type != escptest.TLVType(w.Type) || !bytes.Equal(sets[0][i].Value, w.Value) {
				t.Errorf("TLV %d = %+v, want %+v", i, sets[0][i], w)
			}
		})
	}
	info, err := c.Info(context.Background())
	if err != nil || !info.DHCP || info.Netmask.String() != "255.255.0.0" || info.Gateway.String() != "127.0.0.2" {
		t.Fatalf("after the lease: %+v err=%v", info, err)
	}
	if !c.dest.IP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("the client must be at the leased address, is at %v", c.dest.IP)
	}
}

func TestEnableDHCPWaitsOutTheSilenceWhileTheSwitchAsksForALease(t *testing.T) {
	f := escptest.Start(t)
	f.SetLease(escptest.Lease{IP: escptest.DefaultLease().IP, Mask: escptest.DefaultLease().Mask, Gateway: escptest.DefaultLease().Gateway, Silence: 6})
	c, lookups := dialLeasing(t, f)
	if err := c.EnableDHCP(context.Background()); err != nil {
		t.Fatal(err)
	}
	if *lookups < 2 {
		t.Fatalf("the switch was silent, so the lookup must have been repeated; %d lookup(s)", *lookups)
	}
}

func TestEnableDHCPNeedsTheMACAndSendsNothingWithout(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	if err := c.EnableDHCP(context.Background()); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "Config.MAC") {
		t.Fatalf("got %v, want ErrInvalid naming Config.MAC", err)
	}
	if f.Received() != 0 {
		t.Fatalf("%d packet(s) reached the switch", f.Received())
	}
}

func TestEnableDHCPOnASwitchThatLeasesWritesNothing(t *testing.T) {
	f := escptest.Start(t)
	c, _ := dialLeasing(t, f)
	for range 2 {
		if err := c.EnableDHCP(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.WriteCount(); n != 1 {
		t.Fatalf("the second call must not write: %d writes", n)
	}
}

// The reply to the write was lost but the switch took it: the lookup shows the lease.
func TestEnableDHCPSucceedsWhenTheReplyIsLostButTheSwitchLeases(t *testing.T) {
	f := escptest.Start(t)
	f.Configure(func(f *escptest.Faults) { f.OnSystemSet = escptest.SystemSetLeaves })
	c, _ := dialLeasing(t, f)
	if err := c.EnableDHCP(context.Background()); err != nil {
		t.Fatalf("the switch leases, so the call must succeed: %v", err)
	}
	if n := f.WriteCount(); n != 1 {
		t.Fatalf("the write must be sent once and not repeated, got %d", n)
	}
}

func TestEnableDHCPReportsAWriteThatWasNotApplied(t *testing.T) {
	f := escptest.Start(t)
	f.Configure(func(f *escptest.Faults) { f.DropSets = true })
	c, _ := dialLeasing(t, f, withMoveWait(400*time.Millisecond))
	err := c.EnableDHCP(context.Background())
	if !errors.Is(err, ErrNoReply) || errors.Is(err, ErrMoveUnconfirmed) || !strings.Contains(err.Error(), "not switched on") {
		t.Fatalf("want ErrNoReply saying DHCP was not switched on, got %v", err)
	}
	if f.DroppedSets() == 0 {
		t.Fatal("the write never left the client, so this proves nothing")
	}
}

func TestEnableDHCPReportsASwitchThatAnswersButDoesNotLease(t *testing.T) {
	f := escptest.Start(t)
	c, _ := dialLeasing(t, f, withMoveWait(400*time.Millisecond), withLocator(func(context.Context, MAC) (Info, error) {
		return Info{MAC: escptest.DefaultMAC(), IP: netip.MustParseAddr("127.0.0.1"), Netmask: netip.MustParseAddr("255.255.255.0"), Gateway: netip.MustParseAddr("127.0.0.254")}, nil
	}))
	err := c.EnableDHCP(context.Background())
	if !errors.Is(err, ErrMoveUnconfirmed) || !strings.Contains(err.Error(), "DHCP off") {
		t.Fatalf("want ErrMoveUnconfirmed saying DHCP is off, got %v", err)
	}
}

func TestEnableDHCPReportsASwitchThatCannotBeFound(t *testing.T) {
	f := escptest.Start(t)
	f.Configure(func(f *escptest.Faults) { f.OnSystemSet = escptest.SystemSetVanishes })
	c, _ := dialLeasing(t, f, withMoveWait(400*time.Millisecond))
	err := c.EnableDHCP(context.Background())
	if !errors.Is(err, ErrMoveUnconfirmed) || !strings.Contains(err.Error(), "nobody with the configured MAC answers") {
		t.Fatalf("want ErrMoveUnconfirmed saying nobody answers, got %v", err)
	}
}

func TestEnableDHCPDoesNotWaitAfterARefusal(t *testing.T) {
	f := escptest.Start(t)
	f.Configure(func(f *escptest.Faults) { f.RejectSetsFrom = 1 })
	c, lookups := dialLeasing(t, f)
	var rejected *RejectedError
	if err := c.EnableDHCP(context.Background()); !errors.As(err, &rejected) {
		t.Fatalf("want the switch's refusal, got %v", err)
	}
	if *lookups != 0 {
		t.Fatalf("a refused write was not applied, so no lookup is needed: %d", *lookups)
	}
}

// A caller that gives up after the write was sent still gets the answer: the lookup runs on its own budget.
func TestEnableDHCPSettlesWhenTheCallIsCanceledAfterTheWrite(t *testing.T) {
	f := escptest.Start(t)
	ctx, cancel := context.WithCancel(context.Background())
	f.Configure(func(f *escptest.Faults) {
		f.OnSystemSet = escptest.SystemSetLeaves
		f.AfterSystemSet = cancel
	})
	c, _ := dialLeasing(t, f)
	if err := c.EnableDHCP(ctx); err != nil {
		t.Fatalf("the switch leases, so the call must succeed although the caller gave up: %v", err)
	}
}

// SetIPConfig is the way back: it writes the flag off with the address, and the switch is static there.
func TestSetIPConfigTurnsDHCPOff(t *testing.T) {
	f := escptest.Start(t)
	c, _ := dialLeasing(t, f)
	if err := c.EnableDHCP(context.Background()); err != nil {
		t.Fatal(err)
	}
	static := cfgOf("127.0.0.1", "255.255.255.0", "127.0.0.254")
	if err := c.SetIPConfig(context.Background(), static); err != nil {
		t.Fatal(err)
	}
	info, err := c.Info(context.Background())
	if err != nil || info.DHCP || info.Netmask.String() != "255.255.255.0" || info.Gateway.String() != "127.0.0.254" {
		t.Fatalf("after DHCP off: %+v err=%v", info, err)
	}
	last := f.LastSet()
	if len(last) < 2 || last[1].Type != escptest.TLVType(wire.TLVDHCP) || last[1].Value[0] != 0 {
		t.Fatalf("the write must carry the flag off: %+v", last)
	}
}

// Nothing is written when the switch cannot be read first, or when what it reports about its own
// address is unusable: the request carries that address back.
func TestEnableDHCPWritesNothingWhenTheSwitchCannotBeReadFirst(t *testing.T) {
	f := escptest.Start(t)
	c, _ := dialLeasing(t, f)
	f.Mute()
	if err := c.EnableDHCP(context.Background()); !errors.Is(err, ErrNoReply) {
		t.Fatalf("got %v, want ErrNoReply", err)
	}
	if n := f.WriteCount(); n != 0 {
		t.Fatalf("%d writes reached a switch that was never read", n)
	}

	f = escptest.Start(t)
	defaultMAC := escptest.DefaultMAC()
	f.Configure(func(f *escptest.Faults) {
		f.DiscoverReply = []escptest.Setting{{Type: escptest.TLVType(wire.TLVMAC), Value: defaultMAC[:]}} // no addresses
	})
	c, _ = dialLeasing(t, f)
	if err := c.EnableDHCP(context.Background()); err == nil || !strings.Contains(err.Error(), "unusable IP configuration") {
		t.Fatalf("got %v, want a refusal to write an unusable address back", err)
	}
	if n := f.WriteCount(); n != 0 {
		t.Fatalf("%d writes with an unusable address", n)
	}
}

// A reply that says DHCP is on but carries no usable address is not a lease yet.
func TestEnableDHCPKeepsWaitingForAUsableLease(t *testing.T) {
	f := escptest.Start(t)
	c, _ := dialLeasing(t, f, withMoveWait(400*time.Millisecond), withLocator(func(context.Context, MAC) (Info, error) {
		return Info{MAC: escptest.DefaultMAC(), DHCP: true}, nil
	}))
	err := c.EnableDHCP(context.Background())
	if !errors.Is(err, ErrMoveUnconfirmed) || !strings.Contains(err.Error(), "DHCP on") {
		t.Fatalf("want ErrMoveUnconfirmed saying DHCP is on without an address, got %v", err)
	}
}

// A caller that has already given up sends nothing: not the read before the write, and not the write.
func TestEnableDHCPWithACanceledContextSendsNothing(t *testing.T) {
	f := escptest.Start(t)
	c, lookups := dialLeasing(t, f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.EnableDHCP(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if f.WriteCount() != 0 || *lookups != 0 {
		t.Fatalf("%d write(s) and %d lookup(s) after the caller gave up", f.WriteCount(), *lookups)
	}
}

// A switch that answers at the wanted address but leases it has not taken a static one: the wait
// after SetIPConfig ends only when DHCP is off.
func TestAwaitingAStaticAddressIsNotDoneWhileTheSwitchLeases(t *testing.T) {
	f := escptest.Start(t)
	c, _ := dialLeasing(t, f, withMoveWait(300*time.Millisecond))
	if err := c.EnableDHCP(context.Background()); err != nil { // the fake now reports DHCP on
		t.Fatal(err)
	}
	err := c.awaitAddress(context.Background(), cfgOf("127.0.0.1", "255.255.255.0", "127.0.0.254"))
	if !errors.Is(err, ErrMoveUnconfirmed) {
		t.Fatalf("got %v, want ErrMoveUnconfirmed: the switch is at the address but leases it", err)
	}
}

// After a write that moved the switch the client keys again: the session belonged to the old address.
func TestAfterAMoveTheClientHasNoSession(t *testing.T) {
	f := escptest.Start(t)
	c := dialFast(t, f, "secret")
	if _, err := c.LED(context.Background()); err != nil { // a session exists
		t.Fatal(err)
	}
	if c.session == nil {
		t.Fatal("the premise: a session was set up")
	}
	if err := c.SetIPConfig(context.Background(), lostReplyWanted); err != nil {
		t.Fatal(err)
	}
	if c.session != nil {
		t.Fatal("SetIPConfig kept the session of the old address")
	}
	c.session = []byte("another session")
	if err := c.follow(Info{IP: netip.MustParseAddr("127.0.0.1")}); err != nil {
		t.Fatal(err)
	}
	if c.session != nil {
		t.Fatal("follow kept the session of the old address")
	}
}

// What is said when no lease was reported depends on whether the switch answered, whether DHCP is
// on, and how the write ended.
func TestWhatIsSaidWhenNoLeaseWasReported(t *testing.T) {
	static := Info{IP: netip.MustParseAddr("192.0.2.9"), DHCP: false}
	leasing := Info{IP: netip.MustParseAddr("192.0.2.9"), DHCP: true}
	lost := fmt.Errorf("send: %w", ErrNoReply)
	for _, tt := range []struct {
		name      string
		last      Info
		seen      bool
		lost      error
		moved     bool   // ErrMoveUnconfirmed
		contains  string // in the message
		notSwitch bool   // "so DHCP was not switched on"
	}{
		{"static and the write got no reply: DHCP was not switched on", static, true, lost, false, "DHCP was not switched on", true},
		{"leasing although the write got no reply: not that verdict", leasing, true, lost, true, "DHCP on", false},
		{"static and the write was answered: unconfirmed, DHCP off", static, true, nil, true, "DHCP off", false},
		{"static and the write failed otherwise: unconfirmed", static, true, errors.New("refused"), true, "DHCP off", false},
		{"nobody answers: unconfirmed", Info{}, false, lost, true, "nobody with the configured MAC", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := leaseNotConfirmed(cfgOf("192.0.2.1", "255.255.255.0", "192.0.2.254"), tt.last, tt.seen, tt.lost)
			if errors.Is(err, ErrMoveUnconfirmed) != tt.moved {
				t.Fatalf("ErrMoveUnconfirmed = %v, want %v: %v", !tt.moved, tt.moved, err)
			}
			if got := strings.Contains(err.Error(), "DHCP was not switched on"); got != tt.notSwitch {
				t.Fatalf("says DHCP was not switched on = %v, want %v: %v", got, tt.notSwitch, err)
			}
			if !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("%q does not say %q", err, tt.contains)
			}
		})
	}
}
