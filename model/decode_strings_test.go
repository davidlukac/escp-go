package model

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

// stringDecoder is one string-kind input type seen through its decoder and its rule, so that one table
// drives both types: decode is UnmarshalText on a receiver holding keep, validate is the type's
// Validate on the same text, and kept reports whether the receiver still holds keep.
type stringDecoder struct {
	name   string
	keep   string
	decode func(receiver, text string) (got string, err error)
	// decodeJSON is json.Unmarshal of a JSON string into the type.
	decodeJSON func(doc []byte) (string, error)
	validate   func(text string) error
}

func stringDecoders() []stringDecoder {
	return []stringDecoder{
		{name: "Hostname", keep: "keep-me",
			decode: func(receiver, text string) (string, error) {
				h := Hostname(receiver)
				err := h.UnmarshalText([]byte(text))
				return string(h), err
			},
			decodeJSON: func(doc []byte) (string, error) {
				var h Hostname
				err := json.Unmarshal(doc, &h)
				return string(h), err
			},
			validate: func(text string) error { return Hostname(text).Validate() }},
		{name: "VLANName", keep: "keep_me",
			decode: func(receiver, text string) (string, error) {
				n := VLANName(receiver)
				err := n.UnmarshalText([]byte(text))
				return string(n), err
			},
			decodeJSON: func(doc []byte) (string, error) {
				var n VLANName
				err := json.Unmarshal(doc, &n)
				return string(n), err
			},
			validate: func(text string) error { return VLANName(text).Validate() }},
	}
}

// The texts both types are tried with: what Validate accepts and refuses is taken from Validate, the
// expectations below only name the classes so that a table row that stops meaning what it says is seen.
var stringDecoderTexts = []struct {
	name string
	text string
	// refusedBy lists the types whose rules refuse the text (the two types differ in the underscore and in the empty text).
	refusedBy []string
}{
	{"a plain name", "sw-1", nil},
	{"one letter", "a", nil},
	{"the longest VLAN name", strings.Repeat("a", MaxVLANNameLen), nil},
	{"the longest hostname", strings.Repeat("a", MaxHostnameLen), []string{"VLANName"}},
	{"digits and hyphens inside", "a-1-b", nil},
	{"a space", "bad name", []string{"Hostname", "VLANName"}},
	{"a space and a bang", "bad name!", []string{"Hostname", "VLANName"}},
	{"a leading hyphen", "-ab", []string{"Hostname"}},
	{"a trailing hyphen", "ab-", []string{"Hostname"}},
	{"a dot", "a.b", []string{"Hostname", "VLANName"}},
	{"an underscore", "a_b", []string{"Hostname"}},
	{"a hostname that is one too long", strings.Repeat("a", MaxHostnameLen+1), []string{"Hostname", "VLANName"}},
	{"a VLAN name that is one too long", strings.Repeat("a", MaxVLANNameLen+1), []string{"VLANName"}},
	{"non-ASCII letters", "café", []string{"Hostname", "VLANName"}},
	{"a NUL byte", "a\x00b", []string{"Hostname", "VLANName"}},
	{"a newline", "a\nb", []string{"Hostname", "VLANName"}},
}

// guarantee: Hostname.UnmarshalText
// guarantee: VLANName.UnmarshalText
//
// The decoder of each string type accepts exactly what Validate accepts, plus the empty text; it returns
// Validate's own error, which matches ErrInvalid, and leaves the receiver alone on a refusal.
func TestStringDecodersApplyValidate(t *testing.T) {
	for _, d := range stringDecoders() {
		for _, tc := range stringDecoderTexts {
			t.Run(d.name+"/"+tc.name, func(t *testing.T) {
				want := d.validate(tc.text)
				refused := false
				for _, name := range tc.refusedBy {
					refused = refused || name == d.name
				}
				if (want != nil) != refused {
					t.Fatalf("the table says refused=%v for %q but Validate says %v", refused, tc.text, want)
				}
				got, err := d.decode(d.keep, tc.text)
				if want == nil {
					if err != nil || got != tc.text {
						t.Fatalf("got %q, %v; want %q, nil", got, err, tc.text)
					}
					return
				}
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("the refusal of %q does not match ErrInvalid: %v", tc.text, err)
				}
				if err.Error() != want.Error() {
					t.Errorf("the refusal is %q, Validate says %q", err, want)
				}
				if got != d.keep {
					t.Errorf("the receiver is %q after a refusal, want it left at %q", got, d.keep)
				}
			})
		}
	}
}

// The empty text is the zero value, "not set": it reads, and it replaces what the receiver held (it is a
// value, not a refusal). Validate still decides whether the zero is allowed where it is written.
func TestStringDecodersReadTheEmptyText(t *testing.T) {
	for _, d := range stringDecoders() {
		t.Run(d.name, func(t *testing.T) {
			got, err := d.decode(d.keep, "")
			if err != nil || got != "" {
				t.Fatalf("got %q, %v; want the zero value", got, err)
			}
		})
	}
	if err := Hostname("").Validate(); !errors.Is(err, ErrInvalid) {
		t.Errorf("Hostname(\"\").Validate() = %v; the empty hostname must stay refused as a setting", err)
	}
}

// encoding/json hands a JSON string to UnmarshalText, so the string types need no UnmarshalJSON: a
// struct field, a map value and a map key all go through the rule, with the error of Validate.
func TestStringDecodersThroughJSON(t *testing.T) {
	longHost := strings.Repeat("a", MaxHostnameLen+1)
	for _, tc := range []struct {
		name    string
		doc     string
		into    func() any
		wantErr string // the refusal Validate gives; empty when the document must decode
	}{
		{"Info with a bad hostname", `{"Hostname":"bad name!"}`, func() any { return new(Info) }, Hostname("bad name!").Validate().Error()},
		{"Info with a hostname that is too long", `{"Hostname":"` + longHost + `"}`, func() any { return new(Info) }, Hostname(longHost).Validate().Error()},
		{"Info with a good hostname", `{"Hostname":"sw-1"}`, func() any { return new(Info) }, ""},
		{"Info with an escaped space", `{"Hostname":"a b"}`, func() any { return new(Info) }, Hostname("a b").Validate().Error()},
		{"Info with invalid UTF-8", "{\"Hostname\":\"a\xffb\"}", func() any { return new(Info) }, Hostname("a�b").Validate().Error()},
		{"Dot1QVLAN with a bad name", `{"ID":10,"Name":"bad name!"}`, func() any { return new(Dot1QVLAN) }, VLANName("bad name!").Validate().Error()},
		{"Dot1QVLAN with a good name", `{"ID":10,"Name":"cam_1-a"}`, func() any { return new(Dot1QVLAN) }, ""},
		{"a bad name deep in Dot1Q", `{"VLANs":[{"ID":10,"Name":"ok"},{"ID":11,"Name":"no way"}]}`, func() any { return new(Dot1Q) }, VLANName("no way").Validate().Error()},
		{"a bad VLAN name as a map key", `{"bad key":1}`, func() any { return new(map[VLANName]int) }, VLANName("bad key").Validate().Error()},
		{"a good VLAN name as a map key", `{"good-key":1}`, func() any { return new(map[VLANName]int) }, ""},
		{"an empty VLAN name as a map key", `{"":1}`, func() any { return new(map[VLANName]int) }, ""},
		{"a bad hostname as a map key", `{"bad key":1}`, func() any { return new(map[Hostname]int) }, Hostname("bad key").Validate().Error()},
		{"a bad hostname as a map value", `{"a":"bad name"}`, func() any { return new(map[string]Hostname) }, Hostname("bad name").Validate().Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := json.Unmarshal([]byte(tc.doc), tc.into())
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("got %v, want the document to decode", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("got %v, want an error that matches ErrInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("got %q, want Validate's message %q in it", err, tc.wantErr)
			}
		})
	}
}

// A refusal inside a record leaves the field of the receiver as it was, and a JSON null is a no-op, as
// encoding/json does for every type with UnmarshalText.
func TestStringDecodersThroughJSONLeaveWhatTheyRefuse(t *testing.T) {
	info := Info{Hostname: "keep-me"}
	if err := json.Unmarshal([]byte(`{"Hostname":"bad name!"}`), &info); err == nil {
		t.Fatal("a bad hostname decoded")
	}
	if info.Hostname != "keep-me" {
		t.Errorf("Hostname is %q after a refusal, want it left at keep-me", info.Hostname)
	}
	for _, tc := range []struct {
		name string
		do   func() (string, error)
	}{
		{"Hostname", func() (string, error) {
			v := Info{Hostname: "keep-me"}
			err := json.Unmarshal([]byte(`{"Hostname":null}`), &v)
			return string(v.Hostname), err
		}},
		{"VLANName", func() (string, error) {
			v := Dot1QVLAN{Name: "keep_me"}
			err := json.Unmarshal([]byte(`{"Name":null}`), &v)
			return string(v.Name), err
		}},
	} {
		t.Run("null into "+tc.name, func(t *testing.T) {
			got, err := tc.do()
			if err != nil || !strings.HasPrefix(got, "keep") {
				t.Errorf("got %q, %v; want null to leave the value alone", got, err)
			}
		})
	}
}

// A value that Validate accepts, and the zero value, survive an encoder and a decoder unchanged.
func TestStringTypesRoundTripJSON(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value Dot1QVLAN
	}{
		{"a name", Dot1QVLAN{ID: 10, Name: "cam_1-a"}},
		{"no name", Dot1QVLAN{ID: 10}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			var back Dot1QVLAN
			if err := json.Unmarshal(text, &back); err != nil || back.Name != tc.value.Name {
				t.Fatalf("after %s got %+v, %v", text, back, err)
			}
		})
	}
	for _, name := range []Hostname{"", "sw-1"} {
		t.Run("Info "+string(name), func(t *testing.T) {
			text, err := json.Marshal(Info{Hostname: name})
			if err != nil {
				t.Fatal(err)
			}
			var back Info
			if err := json.Unmarshal(text, &back); err != nil || back.Hostname != name {
				t.Fatalf("after %s got %+v, %v", text, back, err)
			}
		})
	}
}

// A refusal quotes at most maxQuotedBytes of the value: a megabyte of input is not a megabyte of
// error, and the message still names the problem.
func TestRefusalsBoundTheEcho(t *testing.T) {
	const megabyte = 1 << 20
	huge := strings.Repeat("x y", megabyte/3)
	for _, tc := range []struct {
		name string
		err  func() error
		want string
	}{
		{"Hostname.Validate", Hostname(huge).Validate, "invalid device description"},
		{"VLANName.Validate", VLANName(huge).Validate, "invalid VLAN name"},
		{"Hostname.UnmarshalText", func() error { var h Hostname; return h.UnmarshalText([]byte(huge)) }, "invalid device description"},
		{"VLANName.UnmarshalText", func() error { var n VLANName; return n.UnmarshalText([]byte(huge)) }, "invalid VLAN name"},
		{"Hostname through JSON", func() error { return json.Unmarshal([]byte(`{"Hostname":"`+huge+`"}`), new(Info)) }, "invalid device description"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.err()
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("got %v, want ErrInvalid", err)
			}
			msg := err.Error()
			if len(msg) > 300 {
				t.Errorf("the message is %d bytes for a %d byte value, want at most 300", len(msg), len(huge))
			}
			if !strings.Contains(msg, tc.want) || !strings.Contains(msg, "letters, digits") {
				t.Errorf("the message %q does not name the problem (%q ...)", msg, tc.want)
			}
			if !strings.Contains(msg, "... (") || !strings.Contains(msg, "bytes)") {
				t.Errorf("the message %q does not say it quotes only a part", msg)
			}
		})
	}
}

// A value of up to maxQuotedBytes is quoted whole, so the messages of short values are the ones they
// always were; a longer one is cut at a rune boundary and carries its full length.
func TestQuoteBounded(t *testing.T) {
	forty := strings.Repeat("a", maxQuotedBytes)
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", `""`},
		{"short", "bad name!", `"bad name!"`},
		{"escapes are as %q writes them", "a\tb\"c", `"a\tb\"c"`},
		{"exactly the limit is whole", forty, `"` + forty + `"`},
		{"one byte over the limit is cut", forty + "b", `"` + forty + `"... (41 bytes)`},
		{"a multibyte rune is not split", strings.Repeat("a", maxQuotedBytes-1) + "é" + "tail", `"` + strings.Repeat("a", maxQuotedBytes-1) + `"... (45 bytes)`},
		{"a multibyte rune that ends at the limit stays", strings.Repeat("a", maxQuotedBytes-2) + "é" + "tail", `"` + strings.Repeat("a", maxQuotedBytes-2) + "é" + `"... (44 bytes)`},
		{"a four byte rune is not split", strings.Repeat("a", maxQuotedBytes-1) + "\U0001F600" + "tail", `"` + strings.Repeat("a", maxQuotedBytes-1) + `"... (47 bytes)`},
		{"a four byte rune cut at its last byte backs up to its start", strings.Repeat("a", maxQuotedBytes-3) + "\U0001F600" + "tail", `"` + strings.Repeat("a", maxQuotedBytes-3) + `"... (45 bytes)`},
		{"continuation bytes with no rune start are cut after three", strings.Repeat("\x80", maxQuotedBytes+10), `"` + strings.Repeat(`\x80`, maxQuotedBytes-3) + `"... (50 bytes)`},
		{"invalid UTF-8 is cut and escaped", strings.Repeat("\xff", maxQuotedBytes+5), `"` + strings.Repeat(`\xff`, maxQuotedBytes) + `"... (45 bytes)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := quoteBounded(tc.in)
			if got != tc.want {
				t.Errorf("quoteBounded(%d bytes) = %s, want %s", len(tc.in), got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("the quote %q is not valid UTF-8", got)
			}
		})
	}
}
