package model

import (
	"bytes"
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Fuzz targets for the parsers of this package that read text or bytes from outside: names in a
// configuration file or an encoded document, the port mask and the hardware address of a reply.
// `go test` runs the seeds; `go test -fuzz=<Name> -fuzztime=30s ./model` explores further.

// nameParser is one parser of a closed set of names and the text decoder of the same type, both as
// a function from text to the canonical spelling of the value it produced.
type nameParser struct {
	name string
	// parse is the Parse function of the type.
	parse func(string) (string, error)
	// decode is UnmarshalText of the type; nil when the type has none.
	decode func(string) (string, error)
	// extra is the text decode accepts and parse does not (a state a reading reports but nothing
	// can write, and the empty text where it means "none").
	extra []string
}

func nameParsers() []nameParser {
	return []nameParser{
		{name: "QoSMode",
			parse: func(s string) (string, error) { v, err := ParseQoSMode(s); return v.String(), err },
			decode: func(s string) (string, error) {
				var v QoSMode
				err := v.UnmarshalText([]byte(s))
				return v.String(), err
			}},
		{name: "VLANMode",
			parse: func(s string) (string, error) { v, err := ParseVLANMode(s); return string(v), err },
			decode: func(s string) (string, error) {
				var v VLANMode
				err := v.UnmarshalText([]byte(s))
				return string(v), err
			},
			extra: []string{string(VLANModeNone), ""}},
		{name: "Speed",
			parse: func(s string) (string, error) { v, err := ParseSpeed(s); return string(v), err },
			decode: func(s string) (string, error) {
				var v Speed
				err := v.UnmarshalText([]byte(s))
				return string(v), err
			},
			extra: []string{string(SpeedLinkDown), ""}},
		{name: "Impact",
			parse: func(s string) (string, error) { v, err := ParseImpact(s); return v.String(), err },
			decode: func(s string) (string, error) {
				var v Impact
				err := v.UnmarshalText([]byte(s))
				return v.String(), err
			}},
		{name: "Method",
			parse: func(s string) (string, error) { v, err := ParseMethod(s); return v.String(), err },
			decode: func(s string) (string, error) {
				var v Method
				err := v.UnmarshalText([]byte(s))
				return v.String(), err
			}},
		{name: "BuildDate",
			parse: func(s string) (string, error) { v, err := ParseBuildDate(s); return v.String(), err },
			decode: func(s string) (string, error) {
				var v BuildDate
				err := v.UnmarshalText([]byte(s))
				return v.String(), err
			},
			extra: []string{""}},
		{name: "PortID",
			parse: func(s string) (string, error) { v, err := ParsePortID(s); return v.String(), err },
			decode: func(s string) (string, error) {
				var v PortID
				err := v.UnmarshalText([]byte(s))
				return v.String(), err
			},
			extra: []string{"", "0"}},
		{name: "IPv4",
			parse: func(s string) (string, error) { v, err := ParseIPv4(s); return v.String(), err }},
	}
}

// FuzzParseNames: no text makes a name parser panic; a refusal matches ErrInvalid; what a parser
// accepts is the one spelling of its value (so that a name has one text), and the type's text decoder
// agrees with the parser, accepting more only where its documentation says so.
func FuzzParseNames(f *testing.F) {
	for _, s := range []string{
		"", "auto", "100Mbps-full", "link-down", "none", "mtu", "port-based", "802.1q", "cosmetic", "traffic", "management",
		"SetLED", "LED", "20230218", "2023021", "202302180", "３", "0", "1", "01", "+1", "32", "33", "-1", "1.2.3.4", "192.0.2.1", "::1",
		"1.2.3.4%eth0", "999.1.1.1", "dscp/802.1p", "port-based ", "\x00",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, p := range nameParsers() {
			canon, err := p.parse(s)
			if err != nil {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("%s: a refusal of %q does not match ErrInvalid: %v", p.name, s, err)
				}
			} else if canon != s {
				t.Fatalf("%s: %q is accepted but its one spelling is %q", p.name, s, canon)
			}
			if p.decode == nil {
				continue
			}
			decoded, derr := p.decode(s)
			switch {
			case err == nil && (derr != nil || decoded != canon):
				t.Fatalf("%s: Parse accepts %q but UnmarshalText gives %q, %v", p.name, s, decoded, derr)
			case err != nil && derr == nil && !slices.Contains(p.extra, s):
				t.Fatalf("%s: UnmarshalText accepts %q, which Parse refuses and the type does not list", p.name, s)
			case derr != nil && !errors.Is(derr, ErrInvalid):
				t.Fatalf("%s: UnmarshalText refuses %q without ErrInvalid: %v", p.name, s, derr)
			}
		}
	})
}

// FuzzParseFirmware: the version string a switch reports is any text; ParseFirmware never fails,
// keeps the text as it came, and names a build date only when the text holds "Build" and exactly
// eight digits, a date ParseBuildDate accepts; the text codec round trips.
func FuzzParseFirmware(f *testing.F) {
	for _, s := range []string{
		"1.0.0 Build 20230218 Rel.50633", "", "Build 2025071012", "Build 20250710", "Build 20250710x", "Build  20250710", "1.0 Build ２０２５０７１０",
		"Build 20250710 Build 20240101", "\x00Build 20250710", "Build 20250710\n",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		fw := ParseFirmware(s)
		if fw.String() != s || fw.IsZero() != (s == "") {
			t.Fatalf("ParseFirmware(%q) = %q, zero %v", s, fw.String(), fw.IsZero())
		}
		if fw.HasBuild() != (fw.BuildDate() != "") {
			t.Fatalf("ParseFirmware(%q): HasBuild %v with build %q", s, fw.HasBuild(), fw.BuildDate())
		}
		if fw.HasBuild() {
			if _, err := ParseBuildDate(string(fw.BuildDate())); err != nil || !strings.Contains(s, "Build "+string(fw.BuildDate())) {
				t.Fatalf("ParseFirmware(%q) names build %q that is no build date or not in the text: %v", s, fw.BuildDate(), err)
			}
		}
		text, err := fw.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var back Firmware
		if err := back.UnmarshalText(text); err != nil || back != fw {
			t.Fatalf("the text codec turned %q into %q (%v)", s, back.String(), err)
		}
	})
}

// FuzzParsePortMask: the mask of a reply is any bytes; only exactly four are a mask (a refusal matches
// ErrInvalid, so a truncated mask is never the empty set), the ports a mask names are 1 to MaxPort and
// no more than the bits it has, and the mask comes back byte for byte.
func FuzzParsePortMask(f *testing.F) {
	for _, v := range [][]byte{nil, {}, {0, 0, 0, 0}, {0, 0, 0, 0x83}, {0xff, 0xff, 0xff, 0xff}, {0, 0, 1}, {0, 0, 0, 0, 1}} {
		f.Add(v)
	}
	f.Fuzz(func(t *testing.T, v []byte) {
		set, err := ParsePortMask(v)
		if err != nil {
			if !errors.Is(err, ErrInvalid) || set != 0 || len(v) == maskLen {
				t.Fatalf("ParsePortMask(% x) = %v, %v", v, set, err)
			}
			return
		}
		if len(v) != maskLen || !bytes.Equal(set.Mask(), v) {
			t.Fatalf("ParsePortMask(% x) = %v, which does not give the mask back (%x)", v, set, set.Mask())
		}
		ports := set.Ports()
		if len(ports) != set.Len() || len(ports) > MaxPort {
			t.Fatalf("a mask of %d bytes names %d ports (Len %d, at most %d)", len(v), len(ports), set.Len(), MaxPort)
		}
		for _, p := range ports {
			if p.Validate() != nil || !set.Has(p) {
				t.Fatalf("ParsePortMask(% x) names port %d, which is invalid or not in the set", v, p)
			}
		}
	})
}

// FuzzPortSetText: the text of a port list is any text; it never panics, the work is bounded by the
// text (no more ports than the set can hold), and what is accepted survives MarshalText and
// UnmarshalText unchanged.
func FuzzPortSetText(f *testing.F) {
	for _, s := range []string{"", "none", "2,3,5", " 1 , 2 ", "1,1", "32", "33", "0", "-1", ",", "1,,2", "1,2,", "none,1", "+3", "99999999999999999999", strings.Repeat("1,", 200)} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		var set PortSet
		if err := set.UnmarshalText([]byte(s)); err != nil {
			if !errors.Is(err, ErrInvalid) || set != 0 {
				t.Fatalf("UnmarshalText(%q) refused with %v and left %v", s, err, set)
			}
			return
		}
		if set.Len() > MaxPort || set.Len() > strings.Count(s, ",")+1 {
			t.Fatalf("UnmarshalText(%q) gave %d ports from %d entries", s, set.Len(), strings.Count(s, ",")+1)
		}
		text, err := set.MarshalText()
		if err != nil {
			t.Fatal(err)
		}
		var back PortSet
		if err := back.UnmarshalText(text); err != nil || back != set {
			t.Fatalf("%q gave %v, which did not survive its text %q: %v, %v", s, set, text, back, err)
		}
	})
}

// FuzzParseMACBytes: six bytes are a hardware address and no other length is, with the bytes kept;
// UnmarshalText reads what ParseMAC reads (and the empty text as "no MAC"), and what it accepts
// prints back in the one lower-case colon form.
func FuzzParseMACBytes(f *testing.F) {
	f.Add([]byte{0x00, 0x00, 0x5e, 0x00, 0x53, 0x01})
	f.Add([]byte{})
	f.Add([]byte{1, 2, 3, 4, 5})
	f.Add([]byte("00:00:5e:00:53:01"))
	f.Add([]byte("00-00-5E-00-53-01"))
	f.Add([]byte("0000.5e00.5301"))
	f.Add([]byte("00:00:00:00:00:00"))
	f.Fuzz(func(t *testing.T, b []byte) {
		m, ok := MACFromBytes(b)
		if ok != (len(b) == macLen) || (ok && !bytes.Equal(m[:], b)) || (!ok && !m.IsZero()) {
			t.Fatalf("MACFromBytes(% x) = %v, %v", b, m, ok)
		}
		var got MAC
		err := got.UnmarshalText(b)
		want, perr := ParseMAC(string(b))
		switch {
		case len(b) == 0:
			if err != nil || !got.IsZero() {
				t.Fatalf("the empty text must be no MAC, got %v, %v", got, err)
			}
		case (err == nil) != (perr == nil) || (err == nil && got != want):
			t.Fatalf("UnmarshalText(%q) = %v, %v but ParseMAC = %v, %v", b, got, err, want, perr)
		case err == nil && (got.IsZero() || got.String() != strings.ToLower(got.String()) || len(got.String()) != macTextLen):
			t.Fatalf("UnmarshalText(%q) accepted %v", b, got)
		case err != nil && !errors.Is(err, ErrInvalid):
			t.Fatalf("UnmarshalText(%q) refused without ErrInvalid: %v", b, err)
		}
	})
}

// buildDateText is the shape ParseBuildDate promises, written apart from its implementation.
var buildDateText = regexp.MustCompile(`^[0-9]{8}$`)

// FuzzParseBuildDate pins what FuzzParseNames cannot: the accepted set is exactly eight ASCII digits
// (a full-width digit or a trailing newline is not a build date).
func FuzzParseBuildDate(f *testing.F) {
	for _, s := range []string{"20230218", "2023021", "２０２３０２１８", "20230218\n", "", "2023-02-1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_, err := ParseBuildDate(s)
		if (err == nil) != buildDateText.MatchString(s) {
			t.Fatalf("ParseBuildDate(%q) = %v; eight ASCII digits is %v", s, err, buildDateText.MatchString(s))
		}
	})
}
