package model

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
)

// These tests pin the boundaries and branches of the validators in the package that owns them:
// mutation testing runs the tests of a package against that package's own code, so a boundary
// that only a test of the client checks counts as unpinned.

// refused fails the test unless err is nil exactly when bad is false, and matches ErrInvalid otherwise.
func refused(t *testing.T, err error, bad bool) {
	t.Helper()
	switch {
	case bad && !errors.Is(err, ErrInvalid):
		t.Errorf("err = %v, want one that matches ErrInvalid", err)
	case !bad && err != nil:
		t.Errorf("err = %v, want nil", err)
	}
}

type validateCase struct {
	name string
	v    interface{ Validate() error }
	bad  bool
}

func runValidate(t *testing.T, cases []validateCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { refused(t, tc.v.Validate(), tc.bad) })
	}
}

func TestLAGIDValidateBounds(t *testing.T) {
	runValidate(t, []validateCase{
		{"0", LAGID(0), true},
		{"1", LAGID(1), false},
		{"last", LAGID(LAGGroupCount), false},
		{"above the last", LAGID(LAGGroupCount + 1), true},
		{"negative", LAGID(-1), true},
	})
}

func TestLAGIDRange(t *testing.T) {
	for _, tc := range []struct {
		name        string
		g           LAGID
		first, last PortID
	}{
		{"group 1", 1, 1, 4},
		{"group 2", 2, 5, 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, last := tc.g.Range()
			if first != tc.first || last != tc.last {
				t.Errorf("Range() = %d-%d, want %d-%d", first, last, tc.first, tc.last)
			}
		})
	}
}

func TestLAGValidate(t *testing.T) {
	runValidate(t, []validateCase{
		{"invalid group", LAG{ID: 0, Ports: MustPortSet(1, 2)}, true},
		{"removing a group needs only the group", LAG{ID: 2}, false},
		{"one port is too few", LAG{ID: 1, Ports: MustPortSet(1)}, true},
		{"the fewest members", LAG{ID: 1, Ports: MustPortSet(1, 2)}, false},
		{"all four ports of the group", LAG{ID: 1, Ports: MustPortSet(1, 2, 3, 4)}, false},
		{"five ports are too many", LAG{ID: 1, Ports: MustPortSet(1, 2, 3, 4, 5)}, true},
		{"group 1 takes its first and last port", LAG{ID: 1, Ports: MustPortSet(1, 4)}, false},
		{"group 1 does not take the port after its range", LAG{ID: 1, Ports: MustPortSet(3, 5)}, true},
		{"group 2 takes its first and last port", LAG{ID: 2, Ports: MustPortSet(5, 8)}, false},
		{"group 2 does not take the port before its range", LAG{ID: 2, Ports: MustPortSet(4, 5)}, true},
	})
}

func TestPortBandwidthValidate(t *testing.T) {
	runValidate(t, []validateCase{
		{"invalid port", PortBandwidth{Port: 0}, true},
		{"unlimited both ways", PortBandwidth{Port: 1}, false},
		{"a valid rate each way", PortBandwidth{Port: 1, Ingress: RateStep, Egress: MaxBandwidth - MaxBandwidth%RateStep}, false},
		{"invalid ingress", PortBandwidth{Port: 1, Ingress: RateStep + 1}, true},
		{"invalid egress", PortBandwidth{Port: 1, Egress: RateStep + 1}, true},
	})
}

func TestKbpsValidateBounds(t *testing.T) {
	runValidate(t, []validateCase{
		{"negative", -RateStep, true},
		{"unlimited", Kbps(0), false},
		{"not a multiple", RateStep + 1, true},
		{"the highest multiple", MaxBandwidth - MaxBandwidth%RateStep, false},
		{"above the maximum", MaxBandwidth - MaxBandwidth%RateStep + RateStep, true},
	})
}

// guarantee: StormConfig.Validate
func TestStormConfigLimitedAndValidate(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  StormConfig
		want bool
	}{
		{"nothing selected", StormConfig{}, false},
		{"unknown unicast", StormConfig{UnknownUnicast: true}, true},
		{"multicast", StormConfig{Multicast: true}, true},
		{"broadcast", StormConfig{Broadcast: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.Limited(); got != tc.want {
				t.Errorf("Limited() = %v, want %v", got, tc.want)
			}
		})
	}
	runValidate(t, []validateCase{
		{"invalid port", StormConfig{Port: 0}, true},
		{"off needs no rate", StormConfig{Port: 1}, false},
		{"a rate without a type", StormConfig{Port: 1, Rate: RateStep}, true},
		{"a type without a rate", StormConfig{Port: 1, Broadcast: true}, true},
		{"a type with one step", StormConfig{Port: 1, Broadcast: true, Rate: RateStep}, false},
		{"a type with an off-step rate", StormConfig{Port: 1, Broadcast: true, Rate: RateStep + 1}, true},
	})
}

func TestParseIPv4(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		bad  bool
	}{
		{"dotted quad", "192.0.2.1", false},
		{"IPv6", "2001:db8::1", true},
		{"IPv4 in IPv6", "::ffff:192.0.2.1", true},
		{"garbage", "not an address", true},
		{"empty", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, err := ParseIPv4(tc.in)
			refused(t, err, tc.bad)
			if !tc.bad && a != netip.MustParseAddr(tc.in) {
				t.Errorf("ParseIPv4(%q) = %v", tc.in, a)
			}
		})
	}
}

func TestIPConfigValidatePrefixBounds(t *testing.T) {
	cfg := func(mask string) IPConfig {
		return IPConfig{IP: netip.MustParseAddr("10.1.2.3"), Netmask: netip.MustParseAddr(mask), Gateway: netip.MustParseAddr("10.1.2.4")}
	}
	runValidate(t, []validateCase{
		{"/7 is too wide", IPConfig{IP: netip.MustParseAddr("10.1.2.3"), Netmask: netip.MustParseAddr("254.0.0.0"), Gateway: netip.MustParseAddr("10.1.2.4")}, true},
		{"/8", cfg("255.0.0.0"), false},
		{"/24", cfg("255.255.255.0"), false},
		{"/30", IPConfig{IP: netip.MustParseAddr("10.1.2.1"), Netmask: netip.MustParseAddr("255.255.255.252"), Gateway: netip.MustParseAddr("10.1.2.2")}, false},
		{"/31 is too narrow", IPConfig{IP: netip.MustParseAddr("10.1.2.1"), Netmask: netip.MustParseAddr("255.255.255.254"), Gateway: netip.MustParseAddr("10.1.2.0")}, true},
		{"a mask that is not a prefix", cfg("255.0.255.0"), true},
		{"an address in 0.0.0.0/8", IPConfig{IP: netip.MustParseAddr("0.1.2.3"), Netmask: netip.MustParseAddr("255.255.255.0"), Gateway: netip.MustParseAddr("0.1.2.4")}, true},
		{"an address in the reserved 240.0.0.0/4", IPConfig{IP: netip.MustParseAddr("240.1.2.3"), Netmask: netip.MustParseAddr("255.255.255.0"), Gateway: netip.MustParseAddr("240.1.2.4")}, true},
		{"an address of the limited broadcast's block", IPConfig{IP: netip.MustParseAddr("255.255.255.7"), Netmask: netip.MustParseAddr("255.255.255.0"), Gateway: netip.MustParseAddr("255.255.255.8")}, true},
		{"a link-local address", IPConfig{IP: netip.MustParseAddr("169.254.1.2"), Netmask: netip.MustParseAddr("255.255.0.0"), Gateway: netip.MustParseAddr("169.254.1.3")}, true},
		{"the first address below the reserved block", IPConfig{IP: netip.MustParseAddr("239.255.0.9"), Netmask: netip.MustParseAddr("255.255.255.0"), Gateway: netip.MustParseAddr("239.255.0.8")}, true}, // multicast, refused as before
		{"an address just below 240.0.0.0 that is a host", IPConfig{IP: netip.MustParseAddr("223.255.255.9"), Netmask: netip.MustParseAddr("255.255.255.0"), Gateway: netip.MustParseAddr("223.255.255.1")}, false},
		{"an address of 1.0.0.0/8, just above the reserved first block", IPConfig{IP: netip.MustParseAddr("1.2.3.4"), Netmask: netip.MustParseAddr("255.255.255.0"), Gateway: netip.MustParseAddr("1.2.3.1")}, false},
		{"an address that is not IPv4", IPConfig{IP: netip.MustParseAddr("2001:db8::1"), Netmask: netip.MustParseAddr("255.255.255.0"), Gateway: netip.MustParseAddr("10.1.2.4")}, true},
	})
}

func TestMirrorValidate(t *testing.T) {
	runValidate(t, []validateCase{
		{"disabled, empty", Mirror{}, false},
		{"disabled with a destination", Mirror{Destination: 1}, true},
		{"disabled with an ingress port", Mirror{IngressPorts: MustPortSet(2)}, true},
		{"disabled with an egress port", Mirror{EgressPorts: MustPortSet(2)}, true},
		{"enabled with an invalid destination", Mirror{Enabled: true, IngressPorts: MustPortSet(2)}, true},
		{"enabled without sources", Mirror{Enabled: true, Destination: 1}, true},
		{"enabled mirroring itself", Mirror{Enabled: true, Destination: 1, EgressPorts: MustPortSet(1)}, true},
		{"enabled, ingress", Mirror{Enabled: true, Destination: 1, IngressPorts: MustPortSet(2)}, false},
		{"enabled, egress", Mirror{Enabled: true, Destination: 1, EgressPorts: MustPortSet(2)}, false},
	})
}

func TestGroupIsZero(t *testing.T) {
	for _, tc := range []struct {
		name string
		g    Group
		zero bool
	}{
		{"none", Group{}, true},
		{"a LAG", Group{LAG: 1}, false},
		{"a port-based VLAN", Group{PortVLAN: 1}, false},
		{"an 802.1Q VLAN", Group{Dot1QVLAN: 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.g.IsZero(); got != tc.zero {
				t.Errorf("IsZero() = %v, want %v", got, tc.zero)
			}
		})
	}
}

func TestSpeedAllowsFlowControl(t *testing.T) {
	for _, s := range ConfigurableSpeeds() {
		want := s == SpeedAuto || s == Speed1000Full
		t.Run(string(s), func(t *testing.T) {
			if got := s.AllowsFlowControl(); got != want {
				t.Errorf("AllowsFlowControl() = %v, want %v", got, want)
			}
		})
	}
	if SpeedLinkDown.AllowsFlowControl() {
		t.Error("a port with no link must not allow flow control")
	}
}

func TestPortConfigValidate(t *testing.T) {
	runValidate(t, []validateCase{
		{"invalid port", PortConfig{ID: 0, Enabled: true, Speed: SpeedAuto}, true},
		{"auto", PortConfig{ID: 1, Enabled: true, Speed: SpeedAuto}, false},
		{"link-down is only reported", PortConfig{ID: 1, Enabled: true, Speed: SpeedLinkDown}, true},
		{"an unknown speed", PortConfig{ID: 1, Enabled: true, Speed: "warp"}, true},
		{"flow control at a fixed 100 Mbps", PortConfig{ID: 1, Enabled: true, Speed: Speed100Full, FlowControl: true}, true},
		{"flow control at 1000 Mbps", PortConfig{ID: 1, Enabled: true, Speed: Speed1000Full, FlowControl: true}, false},
	})
}

func TestPortIDAsSet(t *testing.T) {
	for _, tc := range []struct {
		p    PortID
		want PortSet
	}{{1, 1}, {2, 2}, {3, 4}, {8, 128}} {
		t.Run(tc.p.String(), func(t *testing.T) {
			if got := tc.p.AsSet(); got != tc.want {
				t.Errorf("AsSet() = %#b, want %#b", got, tc.want)
			}
		})
	}
}

func TestQoSValidate(t *testing.T) {
	runValidate(t, []validateCase{
		{"mode -1", QoSMode(-1), true},
		{"mode 0", QoSMode(0), false},
		{"mode 2", QoSMode(2), false},
		{"mode 3", QoSMode(3), true},
		{"priority below the lowest", PriorityLowest - 1, true},
		{"the lowest priority", PriorityLowest, false},
		{"the highest priority", PriorityHighest, false},
		{"priority above the highest", PriorityHighest + 1, true},
		{"entry with an invalid port", PortPriority{Port: 0, Priority: PriorityLowest}, true},
		{"entry with an invalid priority", PortPriority{Port: 1, Priority: PriorityHighest + 1}, true},
		{"valid entry", PortPriority{Port: 1, Priority: PriorityHighest}, false},
	})
}

func TestHostnameValidateLength(t *testing.T) {
	runValidate(t, []validateCase{
		{"empty", Hostname(""), true},
		{"the longest", Hostname(strings.Repeat("a", MaxHostnameLen)), false},
		{"one too long", Hostname(strings.Repeat("a", MaxHostnameLen+1)), true},
		{"a hyphen inside", Hostname("a-b"), false},
		{"a leading hyphen", Hostname("-ab"), true},
		{"a trailing hyphen", Hostname("ab-"), true},
		{"a dot", Hostname("a.b"), true},
	})
}

func TestVLANIDValidateBounds(t *testing.T) {
	runValidate(t, []validateCase{
		{"below the first", MinVLANID - 1, true},
		{"the first", MinVLANID, false},
		{"the last", MaxVLANID, false},
		{"above the last", MaxVLANID + 1, true},
	})
}

func TestVLANNameValidate(t *testing.T) {
	runValidate(t, []validateCase{
		{"empty", VLANName(""), false},
		{"the longest", VLANName(strings.Repeat("a", MaxVLANNameLen)), false},
		{"one too long", VLANName(strings.Repeat("a", MaxVLANNameLen+1)), true},
		{"digits, hyphen and underscore", VLANName("a1-_"), false},
		{"a space", VLANName("a b"), true},
	})
}

func TestDot1QVLANValidate(t *testing.T) {
	runValidate(t, []validateCase{
		{"invalid id", Dot1QVLAN{ID: 0, Untagged: MustPortSet(1)}, true},
		{"invalid name", Dot1QVLAN{ID: 2, Name: "a b", Untagged: MustPortSet(1)}, true},
		{"a port both tagged and untagged", Dot1QVLAN{ID: 2, Tagged: MustPortSet(1, 2), Untagged: MustPortSet(2, 3)}, true},
		{"no members", Dot1QVLAN{ID: 2}, true},
		{"tagged members only", Dot1QVLAN{ID: 2, Tagged: MustPortSet(1)}, false},
		{"untagged members only", Dot1QVLAN{ID: 2, Untagged: MustPortSet(1)}, false},
	})
}

func TestPortPVIDValidate(t *testing.T) {
	runValidate(t, []validateCase{
		{"invalid port", PortPVID{Port: 0, PVID: 1}, true},
		{"invalid VLAN", PortPVID{Port: 1, PVID: 0}, true},
		{"valid", PortPVID{Port: 1, PVID: 1}, false},
	})
}

func TestDot1QCheckRoom(t *testing.T) {
	table := func(most, entries int) Dot1Q {
		q := Dot1Q{MaxVLANs: most}
		for i := range entries {
			q.VLANs = append(q.VLANs, Dot1QVLAN{ID: VLANID(i + 1)})
		}
		return q
	}
	for _, tc := range []struct {
		name string
		q    Dot1Q
		id   VLANID
		bad  bool
	}{
		{"a switch that did not say its maximum is not bounded", table(0, 5), 100, false},
		{"a full table can change a VLAN it holds", table(3, 3), 2, false},
		{"a full table refuses a new one", table(3, 3), 100, true},
		{"one below the maximum has room", table(3, 2), 100, false},
	} {
		t.Run(tc.name, func(t *testing.T) { refused(t, tc.q.CheckRoom(tc.id), tc.bad) })
	}
}

func TestVLANModeFrom(t *testing.T) {
	mtu, port, q := MTUVLAN{Enabled: true, UplinkPort: 3}, PortBasedVLAN{Enabled: true}, Dot1Q{Enabled: true}
	for _, tc := range []struct {
		name string
		mtu  MTUVLAN
		pb   PortBasedVLAN
		q    Dot1Q
		want VLANModeSetting
	}{
		{"no flag", MTUVLAN{}, PortBasedVLAN{}, Dot1Q{}, VLANModeSetting{Mode: VLANModeNone}},
		{"MTU keeps its uplink port", mtu, PortBasedVLAN{}, Dot1Q{}, VLANModeSetting{Mode: VLANModeMTU, UplinkPort: 3}},
		{"port-based", MTUVLAN{}, port, Dot1Q{}, VLANModeSetting{Mode: VLANModePortBased}},
		{"802.1Q", MTUVLAN{}, PortBasedVLAN{}, q, VLANModeSetting{Mode: VLANModeDot1Q}},
		{"MTU and port-based", mtu, port, Dot1Q{}, VLANModeSetting{Mode: VLANModeNone}},
		{"MTU and 802.1Q", mtu, PortBasedVLAN{}, q, VLANModeSetting{Mode: VLANModeNone}},
		{"port-based and 802.1Q", MTUVLAN{}, port, q, VLANModeSetting{Mode: VLANModeNone}},
		{"all three", mtu, port, q, VLANModeSetting{Mode: VLANModeNone}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := VLANModeFrom(tc.mtu, tc.pb, tc.q); got != tc.want {
				t.Errorf("VLANModeFrom = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestVLANModeSettingValidate(t *testing.T) {
	runValidate(t, []validateCase{
		{"MTU with an uplink port", VLANModeSetting{Mode: VLANModeMTU, UplinkPort: 3}, false},
		{"MTU without one", VLANModeSetting{Mode: VLANModeMTU}, true},
		{"port-based", VLANModeSetting{Mode: VLANModePortBased}, false},
		{"port-based with an uplink port", VLANModeSetting{Mode: VLANModePortBased, UplinkPort: 3}, true},
		{"802.1Q", VLANModeSetting{Mode: VLANModeDot1Q}, false},
		{"802.1Q with an uplink port", VLANModeSetting{Mode: VLANModeDot1Q, UplinkPort: 3}, true},
		{"none cannot be set", VLANModeSetting{Mode: VLANModeNone}, true},
	})
}

func TestPortVLANIDValidateBounds(t *testing.T) {
	runValidate(t, []validateCase{
		{"0", PortVLANID(0), true},
		{"1", PortVLANID(1), false},
		{"the last", PortVLANID(MaxPortVLANID), false},
		{"above the last", PortVLANID(MaxPortVLANID + 1), true},
	})
}

// guarantee: PortVLANLimit
func TestPortVLANLimit(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit PortVLANLimit
		id    PortVLANID
		bad   bool
	}{
		{"unknown limit, the wire's range applies", 0, MaxPortVLANID, false},
		{"unknown limit, above the wire's range", 0, MaxPortVLANID + 1, true},
		{"an id equal to the limit", 8, 8, false},
		{"an id above the limit", 8, 9, true},
		{"an invalid id within the limit", 8, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) { refused(t, tc.limit.Check(tc.id), tc.bad) })
	}
	if PortVLANLimit(0).Known() || !PortVLANLimit(1).Known() {
		t.Error("Known must be true exactly for a positive limit")
	}
}

// guarantee: PortVLANLimit
func TestPortBasedVLANGroupLimitAndGroup(t *testing.T) {
	for _, tc := range []struct {
		name   string
		groups int
		ports  int
		want   PortVLANLimit
	}{
		{"groups above ports", 16, 8, 16},
		{"ports above groups", 4, 8, 8},
		{"neither said", 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := (PortBasedVLAN{MaxGroups: tc.groups}).GroupLimit(System{PortCount: tc.ports}); got != tc.want {
				t.Errorf("GroupLimit = %v, want %v", got, tc.want)
			}
		})
	}
	v := PortBasedVLAN{VLANs: []PortVLAN{{ID: 1, Ports: MustPortSet(1, 2)}, {ID: 3, Ports: MustPortSet(4)}}}
	if ports, ok := v.Group(3); !ok || ports != MustPortSet(4) {
		t.Errorf("Group(3) = %v, %v", ports, ok)
	}
	if ports, ok := v.Group(2); ok || ports != 0 {
		t.Errorf("Group(2) = %v, %v, want nothing", ports, ok)
	}
}
