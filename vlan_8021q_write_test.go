package escp

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
)

func TestDot1QVLANEncodeMatchesVendorLayout(t *testing.T) {
	tests := []struct {
		name string
		v    Dot1QVLAN
		want []byte
	}{
		// id 2, ports 4 and 5 untagged: member mask 0x18, tagged mask 0, name "iot".
		{"untagged", Dot1QVLAN{ID: 2, Name: "iot", Untagged: MustPortSet(4, 5)}, []byte{0, 2, 0, 0, 0, 0x18, 0, 0, 0, 0, 'i', 'o', 't', 0}},
		// id 100, port 1 tagged and port 8 untagged: member 0x81, tagged 0x01, empty name.
		{"mixed", Dot1QVLAN{ID: 100, Tagged: MustPortSet(1), Untagged: MustPortSet(8)}, []byte{0, 100, 0, 0, 0, 0x81, 0, 0, 0, 0x01, 0}},
		// highest id, the highest port of the mask.
		{"edges", Dot1QVLAN{ID: 4094, Name: "a-b_C9", Tagged: MustPortSet(32)}, []byte{0x0f, 0xfe, 0x80, 0, 0, 0, 0x80, 0, 0, 0, 'a', '-', 'b', '_', 'C', '9', 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.v.encode()
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got % x, want % x", got, tt.want)
			}
		})
	}
}

// What the client writes must read back as the same VLAN through the parser used for GETs.
func TestDot1QVLANEncodeRoundTripsThroughParser(t *testing.T) {
	want := Dot1QVLAN{ID: 30, Name: "media", Tagged: MustPortSet(1, 6), Untagged: MustPortSet(2, 3)}
	value := want.encode()
	got, err := parseDot1Q([]wire.TLV{{Type: wire.TLVVLAN8021Q, Value: []byte{1}}, {Type: wire.TLVVLAN8021QVLAN, Value: value}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.VLANs) != 1 || !reflect.DeepEqual(got.VLANs[0], want) {
		t.Fatalf("round trip gave %+v", got.VLANs)
	}
}

func TestDot1QVLANValidation(t *testing.T) {
	tests := []struct {
		name string
		v    Dot1QVLAN
		frag string
	}{
		{"id zero", Dot1QVLAN{ID: 0, Untagged: MustPortSet(1)}, "invalid VLAN id 0"},
		{"id too big", Dot1QVLAN{ID: 4095, Untagged: MustPortSet(1)}, "invalid VLAN id 4095"},
		{"name too long", Dot1QVLAN{ID: 2, Name: "elevenchars", Untagged: MustPortSet(1)}, "invalid VLAN name"},
		{"name with space", Dot1QVLAN{ID: 2, Name: "a b", Untagged: MustPortSet(1)}, "invalid VLAN name"},
		{"both tagged and untagged", Dot1QVLAN{ID: 2, Tagged: MustPortSet(3), Untagged: MustPortSet(3)}, "port 3 is both tagged and untagged"},
		{"no members", Dot1QVLAN{ID: 2}, "no member ports"},
	}
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.SetDot1QVLAN(context.Background(), tt.v)
			if err == nil || !strings.Contains(err.Error(), tt.frag) {
				t.Fatalf("got %v, want an error containing %q", err, tt.frag)
			}
		})
	}
	if err := c.SetDot1QVLAN(context.Background(), Dot1QVLAN{ID: 2}); err == nil {
		t.Fatal("a VLAN without a member port must be rejected")
	}
	if n := f.WriteCount(); n != 0 {
		t.Fatalf("rejected requests reached the switch (%d sets)", n)
	}
}

func TestSetDot1QVLANWire(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	ctx := context.Background()
	if err := c.SetDot1QVLAN(ctx, Dot1QVLAN{ID: 2, Name: "iot", Untagged: MustPortSet(4, 5)}); err != nil {
		t.Fatal(err)
	}
	got := f.LastSet()
	if len(got) != 1 || got[0].Type != wire.TLVVLAN8021QVLAN || !reflect.DeepEqual(got[0].Value, []byte{0, 2, 0, 0, 0, 0x18, 0, 0, 0, 0, 'i', 'o', 't', 0}) {
		t.Fatalf("wire %+v", got)
	}
	if err := c.DeleteDot1QVLAN(ctx, 2); err != nil {
		t.Fatal(err)
	}
	got = f.LastSet()
	if len(got) != 1 || got[0].Type != wire.TLVVLAN8021QVLAN || !reflect.DeepEqual(got[0].Value, []byte{0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("delete wire %+v", got)
	}
	n := f.WriteCount()
	if c.DeleteDot1QVLAN(ctx, 0) == nil || c.DeleteDot1QVLAN(ctx, 4095) == nil {
		t.Fatal("invalid ids must be rejected")
	}
	if f.WriteCount() != n {
		t.Fatal("rejected deletes must not reach the switch")
	}
}

func TestSetPVIDsWire(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	ctx := context.Background()
	if err := c.SetPVIDs(ctx, []PortPVID{{Port: 5, PVID: 300}, {Port: 4, PVID: 2}}); err != nil {
		t.Fatal(err)
	}
	got := f.LastSet()
	// One TLV per port, in port order, each port (1 byte) and pvid (2 bytes, big endian).
	want := []escptest.Setting{{Type: wire.TLVVLAN8021QPVID, Value: []byte{4, 0, 2}}, {Type: wire.TLVVLAN8021QPVID, Value: []byte{5, 1, 0x2c}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("wire %+v, want %+v", got, want)
	}
	n := f.WriteCount()
	for name, bad := range map[string][]PortPVID{
		"empty":        {},
		"port 0":       {{Port: 0, PVID: 2}},
		"port 33":      {{Port: 33, PVID: 2}},
		"pvid 0":       {{Port: 4, PVID: 0}},
		"pvid 4095":    {{Port: 4, PVID: 4095}},
		"a port twice": {{Port: 4, PVID: 2}, {Port: 4, PVID: 3}},
	} {
		if c.SetPVIDs(ctx, bad) == nil {
			t.Fatalf("%s must be rejected", name)
		}
	}
	if f.WriteCount() != n {
		t.Fatal("rejected requests must not reach the switch")
	}
}

// A request with several faults names the first one in the order given, and a port twice is a fault.
func TestSetPVIDsNamesTheFirstFaultInTheOrderGiven(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	err := c.SetPVIDs(context.Background(), []PortPVID{{Port: 3, PVID: 7}, {Port: 6, PVID: 4095}, {Port: 5, PVID: 0}})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "VLAN id 4095") {
		t.Fatalf("want the VLAN id 4095 of port 6 to be named first, got %v", err)
	}
	err = c.SetPVIDs(context.Background(), []PortPVID{{Port: 3, PVID: 7}, {Port: 3, PVID: 8}})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "port 3 appears twice") {
		t.Fatalf("want the repeated port named, got %v", err)
	}
}

func TestPortPVIDValidate(t *testing.T) {
	if err := (PortPVID{Port: 2, PVID: 100}).Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []PortPVID{{Port: 0, PVID: 1}, {Port: 2, PVID: 0}} {
		if err := bad.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}

func TestVLANIDValidate(t *testing.T) {
	for _, id := range []VLANID{MinVLANID, 100, MaxVLANID} {
		if err := id.Validate(); err != nil {
			t.Errorf("VLAN %d: %v", id, err)
		}
	}
	for _, id := range []VLANID{-1, 0, MaxVLANID + 1, 65535} {
		if err := id.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("VLAN %d: got %v, want ErrInvalid", id, err)
		}
	}
	if VLANID(4094).String() != "4094" {
		t.Error("a VLAN id prints as its number")
	}
}

func TestVLANNameValidate(t *testing.T) {
	for _, n := range []VLANName{"", "iot", "a-b_C9", "tenletters"} {
		if err := n.Validate(); err != nil {
			t.Errorf("%q: %v", n, err)
		}
	}
	for _, n := range []VLANName{"elevenchars", "a b", "dot.ted", "é"} {
		if err := n.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: got %v, want ErrInvalid", n, err)
		}
	}
}

func TestVLANNameString(t *testing.T) {
	if got := VLANName("iot-2").String(); got != "iot-2" {
		t.Fatalf("String() = %q, want the name as written", got)
	}
}
