package escp

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
)

// countingPorts scripts a switch that reports n ports (0: no port count in the reply) and counts
// how often the port count was asked for.
func countingPorts(f *escptest.Switch, n int, reads *atomic.Int32) {
	f.Configure(func(f *escptest.Faults) {
		f.OnGet = func(types []escptest.TLVType) ([]escptest.Setting, int32, bool) {
			for _, typ := range types {
				if wire.TLVType(typ) == wire.TLVPortCount {
					reads.Add(1)
					if n == 0 {
						return nil, 0, false
					}
					return []escptest.Setting{{Type: escptest.TLVType(wire.TLVPortCount), Value: []byte{byte(n)}}}, 0, false
				}
			}
			return nil, 0, false
		}
	})
}

// Every writer that names a port refuses one the switch does not have, with ErrInvalid and
// without sending anything. The ports pass ValidatePort (they fit the 32-bit mask), so only the
// switch's own port count can refuse them. The fake is a TL-SG108E: eight ports.
func TestWritersRefusePortsTheSwitchDoesNotHave(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		call func(c *Client) error
	}{
		{"SetPort", func(c *Client) error {
			return c.SetPort(ctx, PortConfig{ID: 9, Enabled: true, Speed: SpeedAuto})
		}},
		{"SetBandwidth", func(c *Client) error { return c.SetBandwidth(ctx, PortBandwidth{Port: 9}) }},
		{"SetStormControl", func(c *Client) error { return c.SetStormControl(ctx, StormConfig{Port: 9}) }},
		{"SetPortMirror destination", func(c *Client) error {
			return c.SetPortMirror(ctx, Mirror{Enabled: true, Destination: 9, IngressPorts: MustPortSet(2)})
		}},
		{"SetPortMirror ingress", func(c *Client) error {
			return c.SetPortMirror(ctx, Mirror{Enabled: true, Destination: 5, IngressPorts: MustPortSet(9)})
		}},
		{"SetPortMirror egress", func(c *Client) error {
			return c.SetPortMirror(ctx, Mirror{Enabled: true, Destination: 5, EgressPorts: MustPortSet(9)})
		}},
		{"SetPortBasedVLAN", func(c *Client) error { return c.SetPortBasedVLAN(ctx, PortVLAN{ID: 2, Ports: MustPortSet(2, 9)}) }},
		{"SetDot1QVLAN tagged", func(c *Client) error {
			return c.SetDot1QVLAN(ctx, Dot1QVLAN{ID: 20, Tagged: MustPortSet(9), Untagged: MustPortSet(2)})
		}},
		{"SetDot1QVLAN untagged", func(c *Client) error {
			return c.SetDot1QVLAN(ctx, Dot1QVLAN{ID: 20, Untagged: MustPortSet(9)})
		}},
		{"SetPVIDs", func(c *Client) error { return c.SetPVIDs(ctx, []PortPVID{{Port: 2, PVID: 1}, {Port: 9, PVID: 1}}) }},
		{"SetVLANMode", func(c *Client) error {
			return c.SetVLANMode(ctx, VLANModeSetting{Mode: VLANModeMTU, UplinkPort: 9})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := escptest.Start(t)
			c := dialFake(t, f, "secret")
			err := tc.call(c)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("want ErrInvalid, got %v", err)
			}
			if !strings.Contains(err.Error(), "port 9 does not exist: this switch has 8 ports") {
				t.Errorf("the message must name the port and the port count: %v", err)
			}
			if n := f.WriteCount(); n != 0 {
				t.Errorf("nothing may be sent for a port that does not exist, the switch received %d writes", n)
			}
		})
	}
}

// A LAG can only name ports of its own group, so the check is shown on a switch with fewer
// ports than the group allows.
func TestSetLAGRefusesPortsBeyondASmallerSwitch(t *testing.T) {
	f := escptest.Start(t)
	var reads atomic.Int32
	countingPorts(f, 4, &reads)
	c := dialFake(t, f, "secret")
	err := c.SetLAG(context.Background(), LAG{ID: 2, Ports: MustPortSet(5, 6)})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "port 5 does not exist: this switch has 4 ports") {
		t.Fatalf("got %v", err)
	}
	if n := f.WriteCount(); n != 0 {
		t.Fatalf("the switch received %d writes", n)
	}
}

// The limit is the switch's, not a fixed one: the last port is accepted, and a larger switch
// accepts ports a smaller one refuses.
func TestPortCountIsTheSwitchsOwn(t *testing.T) {
	ctx := context.Background()
	t.Run("the last port of an eight-port switch is accepted", func(t *testing.T) {
		f := escptest.Start(t)
		c := dialFake(t, f, "secret")
		if err := c.SetPort(ctx, PortConfig{ID: 8, Enabled: true, Speed: SpeedAuto}); err != nil {
			t.Fatal(err)
		}
		if n := f.WriteCount(); n != 1 {
			t.Fatalf("want the one write, got %d", n)
		}
	})
	t.Run("a sixteen-port switch accepts port 9", func(t *testing.T) {
		f := escptest.Start(t)
		var reads atomic.Int32
		countingPorts(f, 16, &reads)
		c := dialFake(t, f, "secret")
		if err := c.SetPort(ctx, PortConfig{ID: 9, Enabled: true, Speed: SpeedAuto}); err != nil {
			t.Fatal(err)
		}
		if err := c.SetPort(ctx, PortConfig{ID: 17, Enabled: true, Speed: SpeedAuto}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("port 17 on sixteen ports: %v", err)
		}
	})
}

// The port count is hardware: it is read once per client, however many writes follow, and a
// write that names no port reads nothing.
func TestPortCountIsReadOnce(t *testing.T) {
	ctx := context.Background()
	f := escptest.Start(t)
	var reads atomic.Int32
	countingPorts(f, 8, &reads)
	c := dialFake(t, f, "secret")
	if err := c.SetLED(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := c.SetLAG(ctx, LAG{ID: 1, Ports: 0}); err != nil { // removing a group names no ports
		t.Fatal(err)
	}
	if n := reads.Load(); n != 0 {
		t.Fatalf("no port was named, yet the port count was read %d time(s)", n)
	}
	for _, id := range []int{2, 3, 4} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			if err := c.SetPort(ctx, PortConfig{ID: PortID(id), Enabled: true, Speed: SpeedAuto}); err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := c.SetBandwidth(ctx, PortBandwidth{Port: 5}); err != nil {
		t.Fatal(err)
	}
	if n := reads.Load(); n != 1 {
		t.Fatalf("the port count must be read once and kept, it was read %d times", n)
	}
}

// If the port count cannot be established the write does not happen, and the error says why.
func TestWriteNeedsThePortCount(t *testing.T) {
	ctx := context.Background()
	t.Run("the reply has no port count", func(t *testing.T) {
		f := escptest.Start(t)
		var reads atomic.Int32
		countingPorts(f, 0, &reads)
		c := dialFake(t, f, "secret")
		err := c.SetPort(ctx, PortConfig{ID: 2, Enabled: true, Speed: SpeedAuto})
		if err == nil || !strings.Contains(err.Error(), "did not report its number of ports") {
			t.Fatalf("got %v", err)
		}
		if errors.Is(err, ErrInvalid) {
			t.Errorf("a switch that does not answer is not the caller's mistake: %v", err)
		}
		if n := f.WriteCount(); n != 0 {
			t.Fatalf("the switch received %d writes", n)
		}
		// nothing was remembered: the next write asks again
		_ = c.SetPort(ctx, PortConfig{ID: 2, Enabled: true, Speed: SpeedAuto})
		if n := reads.Load(); n != 2 {
			t.Errorf("a failed read must not be cached, the count was asked %d times", n)
		}
	})
	t.Run("the switch reports zero ports", func(t *testing.T) {
		f := escptest.Start(t)
		f.Configure(func(f *escptest.Faults) {
			f.OnGet = func([]escptest.TLVType) ([]escptest.Setting, int32, bool) {
				return []escptest.Setting{{Type: escptest.TLVType(wire.TLVPortCount), Value: []byte{0}}}, 0, false
			}
		})
		c := dialFake(t, f, "secret")
		err := c.SetPort(ctx, PortConfig{ID: 1, Enabled: true, Speed: SpeedAuto})
		if err == nil || !strings.Contains(err.Error(), "did not report its number of ports") {
			t.Fatalf("a count of zero is no count, got %v", err)
		}
		if n := f.WriteCount(); n != 0 {
			t.Fatalf("the switch received %d writes", n)
		}
	})
	t.Run("the switch is silent", func(t *testing.T) {
		f := escptest.Start(t)
		f.Configure(func(f *escptest.Faults) {
			f.OnGet = func([]escptest.TLVType) ([]escptest.Setting, int32, bool) { return nil, 0, true }
		})
		c := dialFast(t, f, "secret")
		err := c.SetPort(ctx, PortConfig{ID: 2, Enabled: true, Speed: SpeedAuto})
		if !errors.Is(err, ErrNoReply) {
			t.Fatalf("want ErrNoReply, got %v", err)
		}
		if n := f.WriteCount(); n != 0 {
			t.Fatalf("the switch received %d writes", n)
		}
	})
}

// Two calls that each read a setting and write all of it back must not run at the same time: the
// later write would undo the change of the earlier one (two SetPortPriorities for different ports
// lost one of them; SetHostname wrote back a stale address). The gate holds the first call after
// its read until the second has had every chance to run.
func TestReadModifyWriteCallsDoNotUndoEachOther(t *testing.T) {
	f := escptest.Start(t)
	f.EnableModel(nil)
	var calls atomic.Int32
	inGate := make(chan struct{})
	release := make(chan struct{})
	gate := func(ctx context.Context, sw Reader, op Operation) error {
		if calls.Add(1) == 1 {
			close(inGate)
			select {
			case <-release:
			case <-time.After(10 * time.Second):
			}
		}
		return nil
	}
	c := newBoundClient(t, Config{Host: "127.0.0.1", Username: "admin", Password: "secret", Timeout: 5 * time.Second,
		SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback(), WriteGate: gate}, withLoopback())
	t.Cleanup(func() { c.Close() })
	ctx := context.Background()
	first := make(chan error, 1)
	go func() { first <- c.SetPortPriorities(ctx, []PortPriority{{Port: 3, Priority: PriorityHighest}}) }()
	<-inGate
	second := make(chan error, 1)
	go func() { second <- c.SetPortPriorities(ctx, []PortPriority{{Port: 4, Priority: PriorityHighest}}) }()
	select {
	case err := <-second:
		// Without the lock the second call finishes while the first is held; the first then
		// writes back what it read before, without the second's change.
		close(release)
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(300 * time.Millisecond): // the second waits for the first, as it must
		close(release)
		if err := <-second; err != nil {
			t.Fatal(err)
		}
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	got, err := c.QoS(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range got.Priorities {
		if (p.Port == 3 || p.Port == 4) && p.Priority != PriorityHighest {
			t.Errorf("port %d has priority %d: the change was undone by the other call", p.Port, p.Priority)
		}
	}
}

// A read-modify-write call that is canceled while it waits for another one returns the context's
// error and sends nothing.
func TestReadModifyWriteCallsGiveUpWhenTheirContextEnds(t *testing.T) {
	f := escptest.Start(t)
	f.EnableModel(nil)
	c := dialFake(t, f, "secret")
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []struct {
		name string
		call func() error
	}{
		{"SetHostname", func() error { return c.SetHostname(canceled, "sw-test") }},
		{"SetIPConfig", func() error {
			return c.SetIPConfig(canceled, IPConfig{IP: netip.MustParseAddr("192.0.2.13"), Netmask: netip.MustParseAddr("255.255.255.0"), Gateway: netip.MustParseAddr("192.0.2.1")})
		}},
		{"SetPortPriorities", func() error {
			return c.SetPortPriorities(canceled, []PortPriority{{Port: 3, Priority: PriorityHighest}})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, context.Canceled) {
				t.Fatalf("got %v, want context.Canceled", err)
			}
		})
	}
	if n := len(f.AllSets()); n != 0 {
		t.Fatalf("the canceled calls sent %d writes", n)
	}
}
