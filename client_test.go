package escp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
)

func TestConfigValidate(t *testing.T) {
	mac := MAC{2, 0, 0, 0, 0, 1}
	for _, tc := range []struct {
		name string
		cfg  Config
		want string // the part of the message that names the problem; empty: valid
	}{
		{"a host", Config{Host: "192.0.2.11"}, ""},
		{"a MAC and no host", Config{MAC: mac}, ""},
		{"every optional field set", Config{Host: "192.0.2.11", Timeout: time.Second, Retries: NoRetries, SwitchPort: 1, HostPort: 65535, BroadcastAddr: netip.MustParseAddr("192.0.2.255")}, ""},
		{"the highest switch port", Config{Host: "192.0.2.11", SwitchPort: 65535}, ""},
		{"a host name", Config{Host: "switch.example"}, ""},
		{"a loopback address (a fake switch)", Config{Host: "127.0.0.1"}, ""},
		{"an IPv6 host", Config{Host: "::1"}, `Host "::1"`},
		{"a host with a port", Config{Host: "192.0.2.11:29808"}, `Host "192.0.2.11:29808"`},
		{"a host with a space", Config{Host: "my switch"}, `Host "my switch"`},
		{"the unspecified address", Config{Host: "0.0.0.0"}, "Host 0.0.0.0"},
		{"a multicast address", Config{Host: "224.0.0.1"}, "Host 224.0.0.1"},
		{"the limited broadcast address", Config{Host: "255.255.255.255"}, "Host 255.255.255.255"},
		{"the subnet-like address next to the broadcast", Config{Host: "255.255.255.254"}, ""},
		{"neither a host nor a MAC", Config{Username: "admin"}, "needs a Host or a MAC"},
		{"a negative timeout", Config{Host: "192.0.2.11", Timeout: -time.Second}, "negative Timeout"},
		{"a negative switch port", Config{Host: "192.0.2.11", SwitchPort: -1}, "SwitchPort -1"},
		{"a switch port above the range", Config{Host: "192.0.2.11", SwitchPort: 65536}, "SwitchPort 65536"},
		{"a negative host port", Config{Host: "192.0.2.11", HostPort: -1}, "HostPort -1"},
		{"a host port above the range", Config{Host: "192.0.2.11", HostPort: 70000}, "HostPort 70000"},
		{"the first host port above the range", Config{Host: "192.0.2.11", HostPort: 65536}, "HostPort 65536"},
		{"an IPv6 broadcast address", Config{Host: "192.0.2.11", BroadcastAddr: netip.MustParseAddr("fe80::1")}, "BroadcastAddr fe80::1"},
	} {
		err := tc.cfg.Validate()
		if tc.want == "" {
			if err != nil {
				t.Errorf("%s: %v", tc.name, err)
			}
			continue
		}
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want ErrInvalid naming %q", tc.name, err, tc.want)
		}
	}
}

// Dial refuses a configuration that cannot work, with the same error Validate gives and before it
// looks anything up: the host below would not resolve, so a lookup would say something else.
func TestDialRefusesWhatValidateRefuses(t *testing.T) {
	for _, cfg := range []Config{
		{},
		{Host: "no-such-host.invalid", Timeout: -time.Second},
		{Host: "no-such-host.invalid", HostPort: 99999},
	} {
		c, err := Dial(cfg)
		if c != nil || !errors.Is(err, ErrInvalid) || err.Error() != cfg.Validate().Error() {
			t.Errorf("Dial(%+v) = %v, %v; want ErrInvalid from Validate", cfg, c, err)
		}
	}
}

func TestDialAppliesDefaults(t *testing.T) {
	c, err := Dial(Config{Host: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	if c.cfg.Timeout != 3*time.Second || c.cfg.Retries != 2 || c.cfg.SwitchPort != defaultSwitchPort ||
		c.cfg.HostPort != defaultHostPort || c.cfg.BroadcastAddr != netip.MustParseAddr("255.255.255.255") {
		t.Fatalf("defaults not applied: %+v", c.cfg)
	}
	if c.conn != nil {
		t.Fatal("Dial must not bind the UDP port; that happens on first use")
	}
	if err := c.Close(); err != nil {
		t.Fatalf("closing a client that never opened a socket must succeed: %v", err)
	}
}

func TestDialKeepsExplicitValues(t *testing.T) {
	want := Config{Host: "127.0.0.1", Timeout: time.Second, Retries: 5, SwitchPort: 1111, HostPort: 2222, BroadcastAddr: netip.MustParseAddr("10.0.0.255")}
	c, err := Dial(want)
	if err != nil {
		t.Fatal(err)
	}
	// Config holds a func (the write gate), so it is not comparable with ==; the gate is nil here.
	if !reflect.DeepEqual(c.cfg, want) {
		t.Fatalf("explicit configuration was overwritten: %+v", c.cfg)
	}
}

// Zero selects the default, a negative value selects "no retry"; a positive one is kept.
func TestDialRetriesSemantics(t *testing.T) {
	for _, tc := range []struct{ in, want int }{{0, 2}, {NoRetries, 0}, {-7, 0}, {1, 1}, {5, 5}} {
		c, err := Dial(Config{Host: "127.0.0.1", Retries: tc.in})
		if err != nil {
			t.Fatal(err)
		}
		if c.cfg.Retries != tc.want {
			t.Errorf("Retries %d became %d, want %d", tc.in, c.cfg.Retries, tc.want)
		}
	}
}

// With NoRetries the first lost packet is the answer; with one retry the same loss is absorbed.
func TestNoRetriesSendsEachRequestOnce(t *testing.T) {
	for _, tc := range []struct {
		name    string
		retries int
		wantErr error
	}{{"no retries", NoRetries, ErrNoReply}, {"one retry", 1, nil}} {
		t.Run(tc.name, func(t *testing.T) {
			f := escptest.Start(t)
			f.DropFirst(2) // both datagrams of the first attempt are lost
			c := newBoundClient(t, Config{Host: "127.0.0.1", Timeout: 80 * time.Millisecond, Retries: tc.retries,
				SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback()})
			c.allowLoopback = true
			t.Cleanup(func() { c.Close() })
			_, err := c.Info(context.Background())
			if !errors.Is(err, tc.wantErr) { // errors.Is(nil, nil) is true
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// An IPv6 literal never reaches the lookup: Validate refuses it. A name that does not resolve (the
// .invalid top-level domain never does, RFC 6761) is the lookup's own error, naming the host.
func TestDialRefusesAnIPv6HostAndReportsANameThatDoesNotResolve(t *testing.T) {
	if _, err := Dial(Config{Host: "::1"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("an IPv6 host: got %v, want ErrInvalid", err)
	}
	_, err := Dial(Config{Host: "no-such-host.invalid"})
	if err == nil || errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), `resolving "no-such-host.invalid"`) {
		t.Fatalf("want a resolution error naming the host, got %v", err)
	}
}

func TestCloseReleasesTheSocket(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	if _, err := c.Info(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.conn == nil {
		t.Fatal("the first request must open the socket")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if c.conn != nil {
		t.Fatal("Close must forget the socket")
	}
	// the client is reusable: the next request binds again
	if _, err := c.Info(context.Background()); err != nil {
		t.Fatalf("reuse after Close: %v", err)
	}
}

func TestBusyHostPortIsReportedOnFirstUse(t *testing.T) {
	f := escptest.Start(t)
	busy, err := net.ListenUDP("udp4", &net.UDPAddr{Port: escptest.FreeUDPPort(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := localPort(t, busy)
	c, err := Dial(Config{Host: "127.0.0.1", Username: "admin", Password: "secret", Timeout: 80 * time.Millisecond,
		SwitchPort: f.UDPPort(), HostPort: port, BroadcastAddr: escptest.Loopback()})
	if err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() error{
		"Info":   func() error { _, err := c.Info(context.Background()); return err },
		"LED":    func() error { _, err := c.LED(context.Background()); return err },
		"SetLED": func() error { return c.SetLED(context.Background(), true) },
	} {
		err := call()
		if err == nil || !strings.Contains(err.Error(), "binding UDP port") {
			t.Errorf("%s: want a bind error mentioning the port, got %v", name, err)
		}
	}
}

func TestLocalMAC(t *testing.T) {
	t.Run("loopback has no hardware address", func(t *testing.T) {
		if got := localMAC(net.IPv4(127, 0, 0, 1)); got != [6]byte{} {
			t.Fatalf("got %x, want zero", got)
		}
	})
	t.Run("destination that cannot be dialed", func(t *testing.T) {
		if got := localMAC(net.ParseIP("::1")); got != [6]byte{} {
			t.Fatalf("got %x, want zero", got)
		}
	})
	t.Run("interface with a hardware address", func(t *testing.T) {
		ifs, _ := net.Interfaces()
		for _, ifc := range ifs {
			if len(ifc.HardwareAddr) != 6 {
				continue
			}
			addrs, _ := ifc.Addrs()
			for _, a := range addrs {
				ipn, ok := a.(*net.IPNet)
				if !ok || ipn.IP.To4() == nil || ipn.IP.IsLoopback() {
					continue
				}
				var want [6]byte
				copy(want[:], ifc.HardwareAddr)
				if got := localMAC(ipn.IP); got != want {
					t.Fatalf("localMAC(%s) = %x, want %x of %s", ipn.IP, got, want, ifc.Name)
				}
				return
			}
		}
		t.Skip("no non-loopback IPv4 interface with a hardware address on this host")
	})
}

// The first interface with a hardware address is not the one that holds the address: the lookup
// must follow the address, skip what cannot be listed or has no hardware address, and give zero
// for an address no interface holds. (The test above depends on the host's own interfaces.)
// The lookup a client uses is Locate on this host's interfaces. A zero MAC is refused before
// anything is sent, so the wrapper can be called without touching the network.
func TestHostLocatorIsLocate(t *testing.T) {
	_, err := hostLocator(nil)(context.Background(), MAC{})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("a lookup of the zero MAC = %v, want ErrInvalid", err)
	}
}

func TestMacHolding(t *testing.T) {
	macA, macB := net.HardwareAddr{2, 0, 0, 0, 0, 0xa}, net.HardwareAddr{2, 0, 0, 0, 0, 0xb}
	ifs := []net.Interface{
		{Name: "broken", HardwareAddr: macA},
		{Name: "first", HardwareAddr: macA},
		{Name: "tunnel"}, // no hardware address
		{Name: "second", HardwareAddr: macB},
	}
	held := map[string][]net.Addr{
		"first":  {&net.IPNet{IP: net.IPv4(192, 0, 2, 1), Mask: net.CIDRMask(24, 32)}},
		"tunnel": {&net.IPNet{IP: net.IPv4(192, 0, 2, 7), Mask: net.CIDRMask(24, 32)}},
		"second": {&net.IPAddr{IP: net.IPv4(192, 0, 2, 3)}, &net.IPNet{IP: net.IPv4(192, 0, 2, 2), Mask: net.CIDRMask(24, 32)}},
	}
	addrs := func(i net.Interface) ([]net.Addr, error) {
		if i.Name == "broken" {
			return nil, errors.New("cannot list")
		}
		return held[i.Name], nil
	}
	for _, tc := range []struct {
		name string
		addr net.IP
		want [6]byte
	}{
		{"an address of the first interface", net.IPv4(192, 0, 2, 1), [6]byte(macA)},
		{"an address of a later interface", net.IPv4(192, 0, 2, 2), [6]byte(macB)},
		{"an address that is not a network address", net.IPv4(192, 0, 2, 3), [6]byte{}},
		{"an interface without a hardware address", net.IPv4(192, 0, 2, 7), [6]byte{}},
		{"an address no interface holds", net.IPv4(198, 51, 100, 1), [6]byte{}},
	} {
		if got := macHolding(tc.addr, ifs, addrs); got != tc.want {
			t.Errorf("%s: %x, want %x", tc.name, got, tc.want)
		}
	}
}

// Several clients (several programs or provider instances, one per switch) must be able to
// hold the reply port at the same time.
func TestSeveralClientsShareTheReplyPort(t *testing.T) {
	port := escptest.FreeUDPPort(t)
	var clients []*Client
	for i := range 3 {
		c, err := Dial(Config{Host: "127.0.0.1", HostPort: port})
		if err != nil {
			t.Fatal(err)
		}
		c.mu.Lock()
		err = c.open(context.Background())
		c.mu.Unlock()
		if err != nil {
			t.Fatalf("client %d could not share port %d: %v", i+1, port, err)
		}
		clients = append(clients, c)
	}
	for _, c := range clients {
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDialRefusesABroadcastAddressThatIsNotIPv4(t *testing.T) {
	if _, err := Dial(Config{Host: "127.0.0.1", BroadcastAddr: netip.MustParseAddr("fe80::1")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("got %v, want ErrInvalid", err)
	}
}

// localIP reads the local address of a UDP connection and answers nil for any other kind, instead of
// panicking on a type assertion.
func TestLocalIP(t *testing.T) {
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer udpConn.Close()
	if got := localIP(udpConn); !got.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Errorf("a UDP connection: got %v, want 127.0.0.1", got)
	}
	server, client := net.Pipe() // its local address is not a UDP address
	defer server.Close()
	defer client.Close()
	if got := localIP(client); got != nil {
		t.Errorf("a connection that is not UDP: got %v, want nil", got)
	}
}

// Client and Discover bind the reply port the same way, so a port that is taken reads the same in both.
func TestBindReplyPortExplainsAPortThatIsTaken(t *testing.T) {
	holder, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero}) // exclusive: no sharing option
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	port := localPort(t, holder)
	conn, err := bindReplyPort(context.Background(), port)
	if err == nil {
		conn.Close()
		t.Skip("the system lets a socket share a port that is held exclusively")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("binding UDP port %d", port)) || !strings.Contains(err.Error(), "does not share the port") {
		t.Errorf("got %v", err)
	}
}
