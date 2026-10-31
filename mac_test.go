package escp

import (
	"errors"
	"testing"
)

func TestParseMAC(t *testing.T) {
	want := MAC{0x00, 0x00, 0x5e, 0x00, 0x53, 0x01}
	for _, s := range []string{"00:00:5e:00:53:01", "00:00:5E:00:53:01", "00-00-5e-00-53-01", "00-00-5E-00-53-01"} {
		got, err := ParseMAC(s)
		if err != nil || got != want {
			t.Errorf("ParseMAC(%q) = %v, %v; want %v", s, got, err, want)
		}
	}
	// Refused: not hex, too short, too long, mixed separators, and the forms net.ParseMAC also reads.
	for _, s := range []string{"", "zz:00:5e:00:53:01", "00:00:5e:00:53", "00:00:5e:00:53:01:00:01", "00:00-5e:00:53:01", "00005e005301",
		"0000.5e00.5301", "02:00:5e:10:00:00:00:01", " 00:00:5e:00:53:01", "00:00:5e:00:53:01\n"} {
		if got, err := ParseMAC(s); !errors.Is(err, ErrInvalid) || !got.IsZero() {
			t.Errorf("ParseMAC(%q) = %v, %v; want the zero MAC and ErrInvalid", s, got, err)
		}
	}
}

func TestMACStringAndZero(t *testing.T) {
	m := MAC{0x00, 0x00, 0x5e, 0x00, 0x53, 0x01}
	if m.String() != "00:00:5e:00:53:01" || m.IsZero() {
		t.Fatalf("%v zero=%t", m.String(), m.IsZero())
	}
	if (MAC{}).String() != "" || !(MAC{}).IsZero() {
		t.Fatal("the zero MAC is 'no MAC' and prints as nothing")
	}
	if round, err := ParseMAC(m.String()); err != nil || round != m {
		t.Fatalf("a MAC must survive String and ParseMAC: %v, %v", round, err)
	}
}

func TestMACFromBytes(t *testing.T) {
	if m, ok := MACFromBytes([]byte{1, 2, 3, 4, 5, 6}); !ok || m != (MAC{1, 2, 3, 4, 5, 6}) {
		t.Fatalf("six bytes: %v, %t", m, ok)
	}
	for _, b := range [][]byte{nil, {1, 2, 3}, {1, 2, 3, 4, 5, 6, 7}} {
		if m, ok := MACFromBytes(b); ok || !m.IsZero() {
			t.Errorf("%d bytes accepted: %v", len(b), m)
		}
	}
}
