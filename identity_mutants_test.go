package escp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
)

// Tests for what the extended mutation run (make mutation-extended) showed no test noticed in the
// identity, relocation and exchange code. Each test names the line it holds. The class of the gap
// is DC-C05 (a test that fails when the code is wrong); the code is that of DC-L01 (the context and
// Close: lock, mayExchange, the caller that gave up) and DC-L04 (how a switch is found, named and
// followed: the first contact, the replies accepted while it moves, the interfaces probed).

// A client pinned by MAC that is used for the first time asks the address it has first. When that
// address answers with another device, the answer is final: the switch is not looked up by MAC (the
// lookup would send the client to some third address on the strength of an unauthenticated reply)
// and the wrong device is reported as it is. Held by the second `&&` of the condition of the
// lookup in discover (the one before the parenthesis).
func TestAFirstContactWithAnotherDeviceDoesNotLookTheSwitchUp(t *testing.T) {
	f := escptest.Start(t)
	f.SetMAC(otherMAC)
	relocating := func(c *Client) { c.cfg.RelocateByMAC = true }
	c, lookups := dialMAC(t, f, "127.0.0.1", wantMAC, nil, relocating)
	if !c.mayRelocate() {
		t.Fatal("the client must be allowed to look its switch up, or the test proves nothing")
	}
	_, err := c.Info(context.Background())
	var wrong *WrongSwitchError
	if !errors.Is(err, ErrWrongSwitch) || !errors.As(err, &wrong) || wrong.Want != wantMAC || wrong.Got != otherMAC {
		t.Fatalf("Info = %v, want ErrWrongSwitch naming %v and %v", err, wantMAC, otherMAC)
	}
	if *lookups != 0 {
		t.Fatalf("a device with another MAC answered and the client looked the switch up %d time(s); it must not", *lookups)
	}
}

// The same client does look the switch up when its address does not answer at all: the test above
// is about the kind of failure, and this one shows the lookup is still there to be refused.
func TestAFirstContactThatGetsNoAnswerLooksTheSwitchUp(t *testing.T) {
	f := escptest.Start(t)
	f.Mute()
	relocating := func(c *Client) { c.cfg.RelocateByMAC = true }
	c, lookups := dialMAC(t, f, "127.0.0.1", wantMAC, nil, relocating)
	if _, err := c.Info(context.Background()); err == nil {
		t.Fatal("a muted switch answered")
	}
	if *lookups != 1 {
		t.Fatalf("lookups = %d, want 1 after the silent first contact", *lookups)
	}
}

// cancelOnLog is a slog handler that calls cancel when the library logs the message, and records
// nothing: a way for a test to end the context of a call at a chosen step of it.
type cancelOnLog struct {
	msg    string
	cancel context.CancelFunc
}

func (h cancelOnLog) Enabled(context.Context, slog.Level) bool { return true }
func (h cancelOnLog) Handle(_ context.Context, r slog.Record) error {
	if r.Message == h.msg {
		h.cancel()
	}
	return nil
}
func (h cancelOnLog) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h cancelOnLog) WithGroup(string) slog.Handler      { return h }

// A caller that gives up while the first contact is waiting is not made to wait for a lookup by
// MAC: the silent address is the caller's own end, not a switch that has moved. The context ends
// at the step where the exchange has given up on the last attempt (it logs that it got no reply and
// returns ErrNoReply without looking at the context again), so the error that reaches discover is
// ErrNoReply with a context that is already done, and only the test of the context in discover
// tells the two apart. Held by the first `&&` of the condition in discover.
func TestACallerThatGaveUpDuringTheFirstContactDoesNotStartALookup(t *testing.T) {
	f := escptest.Start(t)
	f.Mute()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	onSilence := func(c *Client) { c.log = slog.New(cancelOnLog{msg: "no reply", cancel: cancel}) }
	relocating := func(c *Client) { c.cfg.RelocateByMAC = true }
	c, lookups := dialMAC(t, f, "127.0.0.1", wantMAC, nil, relocating, onSilence)
	_, err := c.Info(ctx)
	if !errors.Is(err, ErrNoReply) || ctx.Err() == nil {
		t.Fatalf("Info = %v with the context %v; want ErrNoReply, and the context done (the test did not reach its step)", err, ctx.Err())
	}
	if *lookups != 0 {
		t.Fatalf("the caller had given up and the client looked the switch up %d time(s)", *lookups)
	}
}

// A client whose Close has begun but that does not hold the lock yet (Close closes the stop channel
// first and then waits for the lock) refuses to take the lock and to exchange: the three places that
// say so each test the closed flag and the stop channel, and the flag alone is not set yet. The
// state is made by closing the stop channel, which is exactly what Close does first.
// Held by the `||` of lock in request.go and of mayExchange in transport.go.
func TestACallOnAClientWhoseCloseHasBegunIsRefusedBeforeTheLockOrTheSocket(t *testing.T) {
	begun := func(t *testing.T) (*Client, *escptest.Switch) {
		t.Helper()
		f := escptest.Start(t)
		c := dialFake(t, f, "secret")
		c.stopOnce.Do(func() { close(c.stop) }) // Close has begun; it has not got the lock
		if c.closed {
			t.Fatal("the closed flag is set before Close has the lock: the state of the test is wrong")
		}
		return c, f
	}
	t.Run("lock gives nothing and holds nothing", func(t *testing.T) {
		c, _ := begun(t)
		err := c.lock(context.Background())
		if err == nil {
			c.mu.Unlock() // the cleanup of the client waits for the lock
		}
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("lock = %v, want ErrClosed", err)
		}
		if !c.mu.TryLock() {
			t.Fatal("lock refused the call but kept the lock of the client")
		}
		c.mu.Unlock()
	})
	t.Run("mayExchange refuses", func(t *testing.T) {
		c, _ := begun(t)
		if err := c.mayExchange(); !errors.Is(err, ErrClosed) {
			t.Fatalf("mayExchange = %v, want ErrClosed", err)
		}
	})
	t.Run("an exchange sends nothing", func(t *testing.T) {
		c, f := begun(t)
		c.mu.Lock()
		defer c.mu.Unlock()
		if _, err := c.exchange(context.Background(), wire.OpDiscovery, nil, true, c.discoverTargets()); !errors.Is(err, ErrClosed) {
			t.Fatalf("exchange = %v, want ErrClosed", err)
		}
		if f.Received() != 0 {
			t.Fatalf("the fake received %d datagram(s) from a client whose Close had begun", f.Received())
		}
	})
}

// While the switch is being moved the client takes replies from two addresses and from no other:
// the address it is moving to is one more, not a reason to take every reply. Held by the `||` of
// accept in transport.go.
func TestWhileMovingRepliesFromAThirdAddressAreStillRefused(t *testing.T) {
	c, err := Dial(Config{Host: "192.0.2.1", SwitchPort: 1, HostPort: escptest.FreeUDPPort(t)})
	if err != nil {
		t.Fatal(err)
	}
	c.seq = 77
	data := wire.RC4Static(wire.Marshal(wire.Header{Op: wire.OpReply, Seq: 77}, nil))
	tests := []struct {
		name     string
		alsoFrom net.IP
		from     net.IP
		want     bool
	}{
		{"current address, not moving", nil, net.IPv4(192, 0, 2, 1), true},
		{"current address, moving", net.IPv4(192, 0, 2, 2), net.IPv4(192, 0, 2, 1), true},
		{"new address, moving", net.IPv4(192, 0, 2, 2), net.IPv4(192, 0, 2, 2), true},
		{"third address, moving", net.IPv4(192, 0, 2, 2), net.IPv4(192, 0, 2, 3), false},
		{"third address, not moving", nil, net.IPv4(192, 0, 2, 3), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c.alsoFrom = tc.alsoFrom
			if _, ok := c.accept(&net.UDPAddr{IP: tc.from, Port: 1}, data); ok != tc.want {
				t.Fatalf("accept from %v with alsoFrom %v = %v, want %v", tc.from, tc.alsoFrom, ok, tc.want)
			}
		})
	}
}

// The port count is the value of the port-count TLV, not of any one-byte TLV the reply happens to
// carry first. Held by the `&&` of portCount in request.go.
func TestThePortCountIsReadFromThePortCountTLVOnly(t *testing.T) {
	tests := []struct {
		name    string
		reply   []escptest.Setting
		want    int
		wantErr bool
	}{
		{"another one-byte TLV first", []escptest.Setting{
			{Type: escptest.TLVType(wire.TLVLED), Value: []byte{1}},
			{Type: escptest.TLVType(wire.TLVPortCount), Value: []byte{8}},
		}, 8, false},
		{"another one-byte TLV only", []escptest.Setting{
			{Type: escptest.TLVType(wire.TLVLED), Value: []byte{1}},
		}, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := escptest.Start(t)
			f.Configure(func(f *escptest.Faults) {
				f.OnGet = func([]escptest.TLVType) ([]escptest.Setting, int32, bool) { return tc.reply, 0, false }
			})
			c := dialFake(t, f, "secret")
			got, err := c.portCount(context.Background())
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("portCount = %d, %v; want %d, error %v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}

// An interface that cannot be used does not end the list: the ones after it are still looked at,
// whichever reason rules the first one out. Held by the two `continue` of usableInterfaces.
func TestAnUnusableInterfaceDoesNotHideTheOnesAfterIt(t *testing.T) {
	mac := net.HardwareAddr{2, 0, 0, 0, 0, 1}
	const up = net.FlagUp | net.FlagBroadcast
	addrs := map[string][]net.Addr{
		"bad":  {&net.IPNet{IP: net.IPv4(192, 0, 2, 5), Mask: net.CIDRMask(24, 32)}},
		"good": {&net.IPNet{IP: net.IPv4(198, 51, 100, 5), Mask: net.CIDRMask(24, 32)}},
	}
	tests := []struct {
		name string
		bad  net.Interface
	}{
		{"down", net.Interface{Name: "bad", Flags: net.FlagBroadcast, HardwareAddr: mac}},
		{"without broadcast", net.Interface{Name: "bad", Flags: net.FlagUp, HardwareAddr: mac}},
		{"loopback", net.Interface{Name: "bad", Flags: up | net.FlagLoopback, HardwareAddr: mac}},
		{"without a hardware address", net.Interface{Name: "bad", Flags: up}},
		{"addresses that cannot be read", net.Interface{Name: "unreadable", Flags: up, HardwareAddr: mac}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			all := []net.Interface{tc.bad, {Name: "good", Flags: up, HardwareAddr: mac}}
			got := usableInterfaces(all, func(i net.Interface) ([]net.Addr, error) {
				if i.Name == "unreadable" {
					return nil, errors.New("cannot list")
				}
				return addrs[i.Name], nil
			})
			want := []netInterface{{Name: "good", MAC: MAC(mac), Broadcasts: []netip.Addr{netip.MustParseAddr("198.51.100.255")}}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %+v, want %+v", got, want)
			}
		})
	}
}

// A network is IPv4 with a 32-bit mask or it has no broadcast address, whichever of the two is
// wrong. The two mixed forms (an IPv6 address under a 32-bit mask, an IPv4 address under a 128-bit
// one) are not IPv4 networks and must be refused, not computed (the first would index an absent
// address). Held by the first `||` of subnetBroadcast.
func TestSubnetBroadcastRefusesAMaskThatDoesNotFitTheAddress(t *testing.T) {
	tests := []struct {
		name string
		addr net.Addr
	}{
		{"an IPv6 address under a 32-bit mask", &net.IPNet{IP: net.ParseIP("2001:db8::1"), Mask: net.CIDRMask(24, 32)}},
		{"an IPv4 address under a 128-bit mask", &net.IPNet{IP: net.IPv4(192, 0, 2, 1), Mask: net.CIDRMask(120, 128)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := subnetBroadcast(tc.addr); got.IsValid() {
				t.Fatalf("subnetBroadcast = %v, want none", got)
			}
		})
	}
}

// The lookup of a client that was not pointed at one address probes the interfaces of this host, as
// Locate does; only a client with a broadcast address of its own is pointed at that address. So a
// client with the limited broadcast (the default Dial fills in) or none at all probes what the host
// lists, and the lookup of a host with no usable interface says so. The probe never leaves: the
// reply port is held exclusively, so the lookup ends at the bind, after it has said what it would
// have probed. Held by the `&&` of hostLocator in client.go.
func TestTheLookupOfAClientWithTheDefaultBroadcastAddressProbesTheHostsInterfaces(t *testing.T) {
	ifs, err := hostInterfaces()
	if err != nil {
		t.Skipf("this host cannot list its interfaces: %v", err)
	}
	probes := 0
	for _, i := range ifs {
		probes += len(i.Broadcasts)
	}
	if len(ifs) == 1 && probes == 1 {
		t.Skip("this host has one interface with one broadcast address, which the configured one cannot be told from")
	}
	tests := []struct {
		name string
		addr netip.Addr
	}{
		{"the limited broadcast", limitedBroadcast},
		{"no broadcast address", netip.Addr{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			busy, err := net.ListenUDP("udp4", &net.UDPAddr{Port: escptest.FreeUDPPort(t)}) // does not share the port
			if err != nil {
				t.Fatal(err)
			}
			defer busy.Close()
			log, rec := newLogRecorder()
			cfg := Config{BroadcastAddr: tc.addr, SwitchPort: escptest.FreeUDPPort(t), HostPort: localPort(t, busy)}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err = hostLocator(log, cfg)(ctx, wantMAC)
			lines := rec.named("discovery probes")
			if len(ifs) == 0 {
				if !errors.Is(err, ErrNoInterface) || len(lines) != 0 {
					t.Fatalf("a host with no usable interface: err %v, %d probe line(s); want ErrNoInterface and none", err, len(lines))
				}
				return
			}
			if len(lines) != 1 {
				t.Fatalf("%d probe lines (err %v), want 1", len(lines), err)
			}
			got := fmt.Sprintf("%v interfaces, %v probes", lines[0].attrs["interfaces"], lines[0].attrs["probes"])
			if want := fmt.Sprintf("%d interfaces, %d probes", len(ifs), probes); got != want {
				t.Fatalf("the lookup would probe %s, want %s: the interfaces of this host", got, want)
			}
		})
	}
}
