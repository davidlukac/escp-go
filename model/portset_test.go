package model

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestPortIDValidate(t *testing.T) {
	for _, p := range []PortID{1, 8, MaxPort} {
		t.Run(fmt.Sprint(p), func(t *testing.T) {
			if err := p.Validate(); err != nil {
				t.Errorf("port %d: %v", p, err)
			}
		})
	}
	for _, p := range []PortID{-1, 0, MaxPort + 1, 255} {
		t.Run(fmt.Sprint(p), func(t *testing.T) {
			if err := p.Validate(); !errors.Is(err, ErrInvalid) {
				t.Errorf("port %d: got %v, want ErrInvalid", p, err)
			}
		})
	}
	if PortID(7).String() != "7" {
		t.Error("a port prints as its number")
	}
}

func TestParsePortID(t *testing.T) {
	for in, want := range map[string]PortID{"1": 1, "8": 8, "32": MaxPort} {
		t.Run(in, func(t *testing.T) {
			if got, err := ParsePortID(in); err != nil || got != want {
				t.Errorf("ParsePortID(%q) = %d, %v; want %d", in, got, err, want)
			}
		})
	}
	// one port, one spelling: a sign, a leading zero and anything that is not plain decimal digits are refused
	for _, in := range []string{"", "x", "2.5", " 3", "3 ", "0", "-1", "33", "+3", "01", "003", "-0", "0x3", "1_0", "٣", "99999999999999999999"} {
		t.Run(in, func(t *testing.T) {
			if _, err := ParsePortID(in); !errors.Is(err, ErrInvalid) {
				t.Errorf("ParsePortID(%q): got %v, want ErrInvalid", in, err)
			}
		})
	}
}

func TestNewPortSetRefusesAPortOutsideTheMask(t *testing.T) {
	for _, bad := range [][]PortID{{0}, {33}, {1, 2, 40}, {-3}} {
		t.Run(fmt.Sprint(bad), func(t *testing.T) {
			if s, err := NewPortSet(bad...); !errors.Is(err, ErrInvalid) || s != 0 {
				t.Errorf("NewPortSet(%v) = %v, %v; want the empty set and ErrInvalid", bad, s, err)
			}
		})
	}
	// Repeating a port is not an error and does not make it twice.
	if s, err := NewPortSet(2, 2, 5, 2); err != nil || s.Len() != 2 || s.String() != "2,5" {
		t.Fatalf("NewPortSet(2,2,5,2) = %v, %v", s, err)
	}
}

func TestMustPortSetPanicsOnAnInvalidPort(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustPortSet(0) must panic")
		}
	}()
	MustPortSet(0)
}

// The set is the wire bitmap: bit n-1 is port n, big endian.
func TestPortSetIsTheWireMask(t *testing.T) {
	s := MustPortSet(1, 3, 32)
	if got, want := s.Mask(), []byte{0x80, 0, 0, 0x05}; !reflect.DeepEqual(got, want) {
		t.Fatalf("mask % x, want % x", got, want)
	}
	back, err := ParsePortMask(s.Mask())
	if err != nil || back != s {
		t.Fatalf("round trip gave %v, %v", back, err)
	}
}

// A mask of the wrong length is refused; it is never the empty set.
func TestParsePortMaskRefusesTheWrongLength(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []byte
	}{
		{"nil", nil},
		{"three bytes", []byte{1, 2, 3}},
		{"five bytes", []byte{0, 0, 0, 0x0c, 0xff}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePortMask(tc.in)
			if !errors.Is(err, ErrInvalid) || got != 0 {
				t.Fatalf("got %v, %v; want an ErrInvalid and no ports", got, err)
			}
		})
	}
}

// Four zero bytes are a real mask: the empty set.
func TestParsePortMaskAcceptsTheEmptySet(t *testing.T) {
	got, err := ParsePortMask([]byte{0, 0, 0, 0})
	if err != nil || !got.IsEmpty() {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestPortSetOperations(t *testing.T) {
	a, b := MustPortSet(1, 2, 3), MustPortSet(3, 4)
	cases := []struct {
		name      string
		got, want PortSet
	}{
		{"union", a.Union(b), MustPortSet(1, 2, 3, 4)},
		{"intersect", a.Intersect(b), MustPortSet(3)},
		{"minus", a.Minus(b), MustPortSet(1, 2)},
		{"minus itself", a.Minus(a), 0},
		{"union with the empty set", a.Union(0), a},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s: %v, want %v", tc.name, tc.got, tc.want)
			}
		})
	}
	if !a.Has(2) || a.Has(4) || a.Has(0) || a.Has(33) || a.Has(-1) {
		t.Error("Has must be true only for members and false, not a panic, for ports outside the mask")
	}
	// the first and the last port of the mask are members like any other
	ends := MustPortSet(1, MaxPort)
	if !ends.Has(1) || !ends.Has(MaxPort) || ends.Has(2) || ends.Has(MaxPort-1) {
		t.Error("Has must see port 1 and port MaxPort")
	}
	if a.Len() != 3 || a.IsEmpty() || !PortSet(0).IsEmpty() {
		t.Error("Len and IsEmpty")
	}
}

func TestPortSetListsAndPrints(t *testing.T) {
	s := MustPortSet(8, 2, 5)
	if !reflect.DeepEqual(s.Ports(), []PortID{2, 5, 8}) || !reflect.DeepEqual(s.Ints(), []int{2, 5, 8}) {
		t.Fatalf("ascending lists: %v %v", s.Ports(), s.Ints())
	}
	if s.String() != "2,5,8" || PortSet(0).String() != "none" {
		t.Fatalf("String: %q %q", s.String(), PortSet(0).String())
	}
	if got := PortSet(0).Ports(); got == nil || len(got) != 0 {
		t.Fatalf("the empty set lists as a non-nil empty slice, got %#v", got)
	}
}
