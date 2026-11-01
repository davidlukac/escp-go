package escp

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/davidlukac/escp-go/internal/wire"
)

func TestParseDot1QDisabled(t *testing.T) {
	// captured from a TL-SG108E v6 (fw 20230218): 802.1Q off, 32 VLANs max, every port PVID 1
	var tl []wire.TLV
	tl = append(tl, tlvHex(t, wire.TLVVLAN8021Q, "00"), tlvHex(t, wire.TLVVLAN8021QMax, "20"))
	for p := 1; p <= 8; p++ {
		tl = append(tl, wire.TLV{Type: wire.TLVVLAN8021QPVID, Value: []byte{byte(p), 0, 1}})
	}
	got, err := parseDot1Q(tl)
	if err != nil {
		t.Fatal(err)
	}
	if got.Enabled || got.MaxVLANs != 32 || len(got.VLANs) != 0 || len(got.PVIDs) != 8 || got.PVIDs[7] != (PortPVID{Port: 8, PVID: 1}) {
		t.Fatalf("got %+v", got)
	}
}

func TestParseDot1QVLANs(t *testing.T) {
	// layout from the vendor code: VLAN 10 "iot", members 1,2,8, tagged 8
	got, err := parseDot1Q([]wire.TLV{
		tlvHex(t, wire.TLVVLAN8021Q, "01"),
		tlvHex(t, wire.TLVVLAN8021QVLAN, "000a"+"00000083"+"00000080"+"696f7400"),
		tlvHex(t, wire.TLVVLAN8021QVLAN, "0001"+"000000ff"+"00000000"+"44656661756c7400"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Dot1QVLAN{
		{ID: 10, Name: "iot", Tagged: MustPortSet(8), Untagged: MustPortSet(1, 2)},
		{ID: 1, Name: "Default", Tagged: 0, Untagged: MustPortSet(1, 2, 3, 4, 5, 6, 7, 8)},
	}
	if !reflect.DeepEqual(got.VLANs, want) || !got.Enabled {
		t.Fatalf("got %+v", got)
	}
	if _, err := parseDot1Q([]wire.TLV{tlvHex(t, wire.TLVVLAN8021Q, "01"), {Type: wire.TLVVLAN8021QVLAN, Value: []byte{0, 1}}}); err == nil {
		t.Fatal("expected short entry error")
	}
	if _, err := parseDot1Q([]wire.TLV{{Type: wire.TLVVLAN8021QPVID, Value: []byte{1}}}); err == nil {
		t.Fatal("expected error (bad pvid / no state)")
	}
}

// The shortest entry is id, member mask and tagged mask: the name is empty. One byte fewer is cut off.
func TestParseDot1QVLANEntryLengthBoundary(t *testing.T) {
	state := tlvHex(t, wire.TLVVLAN8021Q, "01")
	got, err := parseDot1Q([]wire.TLV{state, tlvHex(t, wire.TLVVLAN8021QVLAN, "0014"+"00000003"+"00000000")})
	if err != nil || len(got.VLANs) != 1 || got.VLANs[0].ID != 20 || got.VLANs[0].Name != "" || got.VLANs[0].Untagged != MustPortSet(1, 2) {
		t.Fatalf("an entry of 10 bytes: %+v, %v", got, err)
	}
	if _, err := parseDot1Q([]wire.TLV{state, tlvHex(t, wire.TLVVLAN8021QVLAN, "0014"+"00000003"+"000000")}); err == nil {
		t.Fatal("an entry of 9 bytes was accepted")
	}
}

func TestDot1QCheckRoom(t *testing.T) {
	table := func(n int) []Dot1QVLAN {
		out := make([]Dot1QVLAN, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, Dot1QVLAN{ID: VLANID(i + 1), Untagged: MustPortSet(1)})
		}
		return out
	}
	for _, c := range []struct {
		name    string
		max     int
		vlans   []Dot1QVLAN
		id      VLANID
		wantErr string // empty: accepted
	}{
		{"room for one more", 32, table(31), 40, ""},
		{"the table is full: a new VLAN is refused", 32, table(32), 40, "table is full: the switch holds at most 32 VLANs (VLAN 1 among them) and has 32, so VLAN 40 cannot be added"},
		{"full, but the VLAN is already there: changing it needs no room", 32, table(32), 7, ""},
		{"full of a smaller table size", 2, table(2), 3, "holds at most 2 VLANs"},
		{"an empty table has room", 32, nil, 20, ""},
		{"a switch that did not report its maximum is not bounded here", 0, table(100), 200, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := Dot1Q{MaxVLANs: c.max, VLANs: c.vlans}.CheckRoom(c.id)
			if c.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("got %v, want ErrInvalid containing %q", err, c.wantErr)
			}
		})
	}
}
