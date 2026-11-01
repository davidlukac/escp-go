package escp

import (
	"encoding/json"
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

// Everything a reader returns survives an encoder: encoding/json writes the text of each value type
// (and so do the YAML and TOML libraries, which use the same interfaces) and reads the same value
// back. A value type without text methods encodes as its representation (a MAC as six numbers, a
// PortSet as its bitmap, a Firmware without its version string), which a person cannot read and a
// decoder cannot always restore.
func TestReadingsSurviveJSON(t *testing.T) {
	mac := MAC{0x00, 0x00, 0x5e, 0x00, 0x53, 0x01}
	for _, tc := range []struct {
		name  string
		value any
		into  any
	}{
		{"Info", Info{Model: "TL-SG108E", Hardware: "TL-SG108E 6.0", Hostname: "sw-1", Firmware: ParseFirmware("1.0.0 Build 20230218 Rel.50633"), MAC: mac,
			IP: netip.MustParseAddr("192.0.2.11"), Netmask: netip.MustParseAddr("255.255.255.0"), Gateway: netip.MustParseAddr("192.0.2.1"), DHCP: true, FactoryDefault: true}, new(Info)},
		{"Info with nothing reported", Info{}, new(Info)},
		{"Firmware without a build", ParseFirmware("something else"), new(Firmware)},
		{"Ports", []Port{{ID: 1, Enabled: true, LAG: 1, ConfiguredSpeed: SpeedAuto, ActualSpeed: Speed1000Full, ConfiguredFlowControl: true, ActualFlowControl: true},
			{ID: 2, ConfiguredSpeed: Speed10Half, ActualSpeed: SpeedLinkDown}}, new([]Port)},
		{"LAGs", []LAG{{ID: 1, Ports: MustPortSet(1, 2, 3)}, {ID: 2}}, new([]LAG)},
		{"Mirror", Mirror{Enabled: true, Destination: 8, IngressPorts: MustPortSet(2, 3), EgressPorts: MustPortSet(4)}, new(Mirror)},
		{"Bandwidth", []PortBandwidth{{Port: 1, Ingress: 640, Egress: 1280}}, new([]PortBandwidth)},
		{"StormControl", []PortStorm{{Port: 2, Broadcast: true, Rate: 64, Enabled: true}}, new([]PortStorm)},
		{"QoS", QoS{Mode: QoSDSCP8021p, Priorities: []PortPriority{{Port: 1, Priority: PriorityLowest}, {Port: 2, Priority: PriorityHighest}}}, new(QoS)},
		{"IGMP", IGMP{Enabled: true, ReportSuppression: true, Groups: []IGMPGroup{{IP: netip.MustParseAddr("239.1.1.1"), VLAN: 1, Ports: MustPortSet(2, 5)}}}, new(IGMP)},
		{"VLAN mode", VLANModeSetting{Mode: VLANModeMTU, UplinkPort: 8}, new(VLANModeSetting)},
		{"VLAN mode none", VLANModeSetting{Mode: VLANModeNone}, new(VLANModeSetting)},
		{"MTU VLAN", MTUVLAN{Enabled: true, UplinkPort: 8}, new(MTUVLAN)},
		{"Port-based VLAN", PortBasedVLAN{Enabled: true, MaxGroups: 8, VLANs: []PortVLAN{{ID: 1, Ports: MustPortSet(1, 2)}}}, new(PortBasedVLAN)},
		{"802.1Q", Dot1Q{Enabled: true, MaxVLANs: 32, VLANs: []Dot1QVLAN{{ID: 10, Name: "cameras", Tagged: MustPortSet(1), Untagged: MustPortSet(2, 3)}},
			PVIDs: []PortPVID{{Port: 2, PVID: 10}}}, new(Dot1Q)},
		{"Port statistics", []PortStats{{ID: 1, Enabled: true, Link: Speed100Full, TxGood: 4294967295, RxGood: 7}}, new([]PortStats)},
		{"System", System{PortCount: 8, AutoSave: true}, new(System)},
		{"Operation", Operation{Name: "SetLAG", Ports: MustPortSet(2, 3), Impact: ImpactManagement, Replaces: Group{LAG: 1}}, new(Operation)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(text, tc.into); err != nil {
				t.Fatalf("decoding %s: %v", text, err)
			}
			if got := reflect.ValueOf(tc.into).Elem().Interface(); !reflect.DeepEqual(got, tc.value) {
				t.Errorf("after %s\ngot  %+v\nwant %+v", text, got, tc.value)
			}
		})
	}
}

// What the encoded text looks like is a promise too: a person reads it and another program parses it.
func TestEncodedValuesAreReadable(t *testing.T) {
	mac := MAC{0x00, 0x00, 0x5e, 0x00, 0x53, 0x01}
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{"MAC", mac, `"00:00:5e:00:53:01"`},
		{"zero MAC", MAC{}, `""`},
		{"PortSet", MustPortSet(2, 3, 5), `"2,3,5"`},
		{"empty PortSet", PortSet(0), `"none"`},
		{"Firmware", ParseFirmware("1.0.0 Build 20230218 Rel.50633"), `"1.0.0 Build 20230218 Rel.50633"`},
		{"QoSMode", QoSPortBased, `"port-based"`},
		{"Impact", ImpactTraffic, `"traffic"`},
		{"Speed", Speed100Full, `"100Mbps-full"`},
		{"VLANMode", VLANModeDot1Q, `"802.1q"`},
	} {
		got, err := json.Marshal(tc.value)
		if err != nil || string(got) != tc.want {
			t.Errorf("%s: got %s, %v, want %s", tc.name, got, err, tc.want)
		}
	}
}

// Text that is no value is refused with ErrInvalid, and a value that has no text is not encoded
// into text that cannot be read back.
func TestDecodingRefusesWhatIsNoValue(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		into any
	}{
		{"a MAC with dots", `"0000.5e00.5301"`, new(MAC)},
		{"a port set with a word", `"2,x"`, new(PortSet)},
		{"a port set with port 0", `"0,2"`, new(PortSet)},
		{"a port set with port 33", `"2,33"`, new(PortSet)},
		{"a QoS mode nobody knows", `"fast"`, new(QoSMode)},
		{"an impact nobody knows", `"huge"`, new(Impact)},
		{"a speed nobody knows", `"warp"`, new(Speed)},
		{"a VLAN mode nobody knows", `"vxlan"`, new(VLANMode)},
		{"a build date with letters", `"2023021a"`, new(BuildDate)},
	} {
		err := json.Unmarshal([]byte(tc.text), tc.into)
		if err == nil || !strings.Contains(err.Error(), "escp:") {
			t.Errorf("%s: got %v, want an escp error", tc.name, err)
		}
	}
	if _, err := json.Marshal(QoSMode(9)); err == nil {
		t.Error("QoSMode(9) was encoded")
	}
	if _, err := json.Marshal(Impact(0)); err == nil {
		t.Error("Impact(0) was encoded")
	}
}

// The empty build date is the value "no build date", and reads back as it was written.
func TestTheEmptyBuildDateSurvivesJSON(t *testing.T) {
	text, err := json.Marshal(struct{ B BuildDate }{})
	if err != nil {
		t.Fatal(err)
	}
	var back struct{ B BuildDate }
	if err := json.Unmarshal(text, &back); err != nil || back.B != "" {
		t.Fatalf("got %+v, %v", back, err)
	}
}

func TestEveryNamedValueReadsBackFromItsText(t *testing.T) {
	for _, s := range append(ConfigurableSpeeds(), SpeedLinkDown) {
		var back Speed
		if err := back.UnmarshalText([]byte(s)); err != nil || back != s {
			t.Errorf("speed %q: %q, %v", s, back, err)
		}
	}
	for _, m := range append(VLANModes(), VLANModeNone) {
		var back VLANMode
		if err := back.UnmarshalText([]byte(m)); err != nil || back != m {
			t.Errorf("VLAN mode %q: %q, %v", m, back, err)
		}
	}
	for _, m := range QoSModes() {
		text, err := m.MarshalText()
		var back QoSMode
		if err != nil || back.UnmarshalText(text) != nil || back != m {
			t.Errorf("QoS mode %v: %q, %v", m, text, err)
		}
	}
	for _, i := range []Impact{ImpactCosmetic, ImpactTraffic, ImpactManagement} {
		text, err := i.MarshalText()
		var back Impact
		if err != nil || back.UnmarshalText(text) != nil || back != i {
			t.Errorf("impact %v: %q, %v", i, text, err)
		}
	}
}

// A port list reads with spaces, repeats and "none"; every port set reads back from its own text.
func TestPortSetTextForms(t *testing.T) {
	for _, tc := range []struct {
		text string
		want PortSet
	}{{"", 0}, {"none", 0}, {" 2 , 3 ", MustPortSet(2, 3)}, {"3,2,3", MustPortSet(2, 3)}, {"32", MustPortSet(32)}} {
		var got PortSet
		if err := got.UnmarshalText([]byte(tc.text)); err != nil || got != tc.want {
			t.Errorf("%q: got %v, %v, want %v", tc.text, got, err, tc.want)
		}
	}
	for bits := range PortSet(1 << 10) { // every set of the first ten ports
		set := bits
		text, _ := set.MarshalText()
		var back PortSet
		if err := back.UnmarshalText(text); err != nil || back != set {
			t.Fatalf("%v: %q read back as %v, %v", set, text, back, err)
		}
	}
}

// The named values validate like every other input type: Validate returns an error that matches
// ErrInvalid, and refuses what the writers refuse (the reported-only SpeedLinkDown and VLANModeNone).
func TestNamedValuesValidate(t *testing.T) {
	for _, s := range ConfigurableSpeeds() {
		if err := s.Validate(); err != nil {
			t.Errorf("speed %q: %v", s, err)
		}
	}
	for _, s := range []Speed{SpeedLinkDown, "", "warp"} {
		if err := s.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("speed %q: got %v, want ErrInvalid", s, err)
		}
	}
	for _, m := range VLANModes() {
		if err := m.Validate(); err != nil {
			t.Errorf("VLAN mode %q: %v", m, err)
		}
	}
	for _, m := range []VLANMode{VLANModeNone, "", "vxlan"} {
		if err := m.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("VLAN mode %q: got %v, want ErrInvalid", m, err)
		}
	}
}
