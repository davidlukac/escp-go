package escp

import (
	"context"
	"errors"
	"testing"

	"github.com/davidlukac/escp-go/escptest"
)

// Every value a writer takes can be checked before it is written, and the writer refuses exactly
// what the check refuses, with the same error: a front end that validates early and a client that
// validates late must not disagree.
func TestValidatorsAndWritersAgree(t *testing.T) {
	f := escptest.Start(t)
	f.EnableModel(nil)
	c := dialFake(t, f, "secret")
	ctx := context.Background()
	ports := MustPortSet

	for _, tc := range []struct {
		name     string
		validate func() error
		write    func() error
		valid    bool
	}{
		{"port: auto speed", PortConfig{ID: 3, Enabled: true, Speed: SpeedAuto}.Validate, func() error { return c.SetPort(ctx, PortConfig{ID: 3, Enabled: true, Speed: SpeedAuto}) }, true},
		{"port: flow control at auto", PortConfig{ID: 3, Speed: SpeedAuto, FlowControl: true}.Validate, func() error { return c.SetPort(ctx, PortConfig{ID: 3, Speed: SpeedAuto, FlowControl: true}) }, true},
		{"port: port 0", PortConfig{Speed: SpeedAuto}.Validate, func() error { return c.SetPort(ctx, PortConfig{Speed: SpeedAuto}) }, false},
		{"port: no speed", PortConfig{ID: 3}.Validate, func() error { return c.SetPort(ctx, PortConfig{ID: 3}) }, false},
		{"port: link-down is no setting", PortConfig{ID: 3, Speed: SpeedLinkDown}.Validate, func() error { return c.SetPort(ctx, PortConfig{ID: 3, Speed: SpeedLinkDown}) }, false},
		{"port: flow control at 100M", PortConfig{ID: 3, Speed: Speed100Full, FlowControl: true}.Validate, func() error { return c.SetPort(ctx, PortConfig{ID: 3, Speed: Speed100Full, FlowControl: true}) }, false},

		{"storm: a type and a rate", StormConfig{Port: 3, Broadcast: true, Rate: 128}.Validate, func() error { return c.SetStormControl(ctx, StormConfig{Port: 3, Broadcast: true, Rate: 128}) }, true},
		{"storm: off", StormConfig{Port: 3}.Validate, func() error { return c.SetStormControl(ctx, StormConfig{Port: 3}) }, true},
		{"storm: port 0", StormConfig{Broadcast: true, Rate: 128}.Validate, func() error { return c.SetStormControl(ctx, StormConfig{Broadcast: true, Rate: 128}) }, false},
		{"storm: a type and no rate", StormConfig{Port: 3, Multicast: true}.Validate, func() error { return c.SetStormControl(ctx, StormConfig{Port: 3, Multicast: true}) }, false},
		{"storm: a rate off the step", StormConfig{Port: 3, Multicast: true, Rate: 100}.Validate, func() error { return c.SetStormControl(ctx, StormConfig{Port: 3, Multicast: true, Rate: 100}) }, false},
		{"storm: a rate and no type", StormConfig{Port: 3, Rate: 128}.Validate, func() error { return c.SetStormControl(ctx, StormConfig{Port: 3, Rate: 128}) }, false},

		{"bandwidth: both directions", PortBandwidth{Port: 3, Ingress: 128, Egress: 256}.Validate, func() error { return c.SetBandwidth(ctx, PortBandwidth{Port: 3, Ingress: 128, Egress: 256}) }, true},
		{"bandwidth: unlimited", PortBandwidth{Port: 3}.Validate, func() error { return c.SetBandwidth(ctx, PortBandwidth{Port: 3}) }, true},
		{"bandwidth: port 0", PortBandwidth{Ingress: 128}.Validate, func() error { return c.SetBandwidth(ctx, PortBandwidth{Ingress: 128}) }, false},
		{"bandwidth: a bad ingress rate", PortBandwidth{Port: 3, Ingress: 100}.Validate, func() error { return c.SetBandwidth(ctx, PortBandwidth{Port: 3, Ingress: 100}) }, false},
		{"bandwidth: a bad egress rate", PortBandwidth{Port: 3, Egress: MaxBandwidthKbps + RateStepKbps}.Validate, func() error {
			return c.SetBandwidth(ctx, PortBandwidth{Port: 3, Egress: MaxBandwidthKbps + RateStepKbps})
		}, false},

		{"mirror: off", Mirror{}.Validate, func() error { return c.SetPortMirror(ctx, Mirror{}) }, true},
		{"mirror: on", Mirror{Enabled: true, Destination: 5, IngressPorts: ports(2)}.Validate, func() error {
			return c.SetPortMirror(ctx, Mirror{Enabled: true, Destination: 5, IngressPorts: ports(2)})
		}, true},
		{"mirror: off with a port", Mirror{Destination: 5}.Validate, func() error { return c.SetPortMirror(ctx, Mirror{Destination: 5}) }, false},
		{"mirror: off with sources", Mirror{EgressPorts: ports(2)}.Validate, func() error { return c.SetPortMirror(ctx, Mirror{EgressPorts: ports(2)}) }, false},
		{"mirror: no destination", Mirror{Enabled: true, IngressPorts: ports(2)}.Validate, func() error { return c.SetPortMirror(ctx, Mirror{Enabled: true, IngressPorts: ports(2)}) }, false},
		{"mirror: no source", Mirror{Enabled: true, Destination: 5}.Validate, func() error { return c.SetPortMirror(ctx, Mirror{Enabled: true, Destination: 5}) }, false},
		{"mirror: itself", Mirror{Enabled: true, Destination: 5, EgressPorts: ports(4, 5)}.Validate, func() error {
			return c.SetPortMirror(ctx, Mirror{Enabled: true, Destination: 5, EgressPorts: ports(4, 5)})
		}, false},

		{"802.1Q VLAN: tagged and untagged", Dot1QVLAN{ID: 20, Name: "iot", Tagged: ports(8), Untagged: ports(2)}.Validate, func() error {
			return c.SetDot1QVLAN(ctx, Dot1QVLAN{ID: 20, Name: "iot", Tagged: ports(8), Untagged: ports(2)})
		}, true},
		{"802.1Q VLAN: id 0", Dot1QVLAN{Untagged: ports(2)}.Validate, func() error { return c.SetDot1QVLAN(ctx, Dot1QVLAN{Untagged: ports(2)}) }, false},
		{"802.1Q VLAN: a name too long", Dot1QVLAN{ID: 20, Name: "far-too-long-a-name", Untagged: ports(2)}.Validate, func() error {
			return c.SetDot1QVLAN(ctx, Dot1QVLAN{ID: 20, Name: "far-too-long-a-name", Untagged: ports(2)})
		}, false},
		{"802.1Q VLAN: a port both ways", Dot1QVLAN{ID: 20, Tagged: ports(2), Untagged: ports(2)}.Validate, func() error { return c.SetDot1QVLAN(ctx, Dot1QVLAN{ID: 20, Tagged: ports(2), Untagged: ports(2)}) }, false},
		{"802.1Q VLAN: no member", Dot1QVLAN{ID: 20}.Validate, func() error { return c.SetDot1QVLAN(ctx, Dot1QVLAN{ID: 20}) }, false},
	} {
		want := tc.validate()
		if tc.valid {
			if want != nil {
				t.Errorf("%s: Validate = %v, want nil", tc.name, want)
			}
			continue
		}
		if !errors.Is(want, ErrInvalid) {
			t.Errorf("%s: Validate = %v, want ErrInvalid", tc.name, want)
			continue
		}
		before := f.WriteCount()
		if got := tc.write(); got == nil || got.Error() != want.Error() || f.WriteCount() != before {
			t.Errorf("%s: the writer gave %v after %d writes, want the error of Validate (%v) and no write", tc.name, got, f.WriteCount()-before, want)
		}
	}
}

func TestParseVLANMode(t *testing.T) {
	for _, m := range VLANModes() {
		if got, err := ParseVLANMode(string(m)); err != nil || got != m {
			t.Errorf("ParseVLANMode(%q) = %q, %v", m, got, err)
		}
	}
	for _, name := range []string{"", "none", "MTU", "802.1Q", "port based", "bogus"} {
		if got, err := ParseVLANMode(name); !errors.Is(err, ErrInvalid) || got != "" {
			t.Errorf("ParseVLANMode(%q) = %q, %v; want an ErrInvalid and no mode", name, got, err)
		}
	}
}
