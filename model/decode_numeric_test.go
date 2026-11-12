package model

import (
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Tests of the decoders of the six numeric input types (PortID, LAGID, VLANID, PortVLANID, Priority,
// Kbps): they read what Validate accepts, plus zero, and refuse the rest with Validate's own error
// (DC-L05, escp-go#216).

// numericPtr is the pointer to a numeric type with its two decoders.
type numericPtr[T any] interface {
	*T
	encoding.TextUnmarshaler
	json.Unmarshaler
}

// numericDecoder is the decoders of one numeric type seen from outside, whichever the type is: every
// function holds seed in the receiver (or the field, or the map) first and returns what it holds
// after the decode.
type numericDecoder struct {
	name string
	// valid and invalid are numbers Validate accepts (not 0) and refuses.
	valid, invalid []int
	validate       func(int) error
	text, jsn      func(seed int, in string) (int, error)
	// field decodes the JSON document {"V": <in>} into a struct with one field of the type.
	field func(seed int, in string) (int, error)
	// key decodes the JSON document {"<in>": "x"} into a map keyed by the type; it returns the key.
	key func(in string) (int, error)
	// encode is the JSON of a struct with one field of the type holding n, and of a map keyed by n.
	encode func(n int) (field, key string, err error)
}

func newNumericDecoder[T numeric, P numericPtr[T]](name string, valid, invalid []int) numericDecoder {
	return numericDecoder{
		name: name, valid: valid, invalid: invalid,
		validate: func(n int) error { return T(n).Validate() },
		text: func(seed int, in string) (int, error) {
			v := T(seed)
			err := P(&v).UnmarshalText([]byte(in))
			return int(v), err
		},
		jsn: func(seed int, in string) (int, error) {
			v := T(seed)
			err := P(&v).UnmarshalJSON([]byte(in))
			return int(v), err
		},
		field: func(seed int, in string) (int, error) {
			doc := struct{ V T }{V: T(seed)}
			err := json.Unmarshal([]byte(`{"V":`+in+`}`), &doc)
			return int(doc.V), err
		},
		key: func(in string) (int, error) {
			var doc map[T]string
			if err := json.Unmarshal([]byte(`{`+strconv.Quote(in)+`:"x"}`), &doc); err != nil {
				return 0, err
			}
			keys := slices.Collect(maps.Keys(doc))
			if len(keys) != 1 {
				return 0, fmt.Errorf("the map holds %d keys", len(keys))
			}
			return int(keys[0]), nil
		},
		encode: func(n int) (string, string, error) {
			f, err := json.Marshal(struct{ V T }{V: T(n)})
			if err != nil {
				return "", "", err
			}
			k, err := json.Marshal(map[T]string{T(n): "x"})
			return string(f), string(k), err
		},
	}
}

// numericDecoders is the six types with numbers at and around the edges of their ranges.
func numericDecoders() []numericDecoder {
	return []numericDecoder{
		newNumericDecoder[PortID]("PortID", []int{1, 2, 8, MaxPort}, []int{MaxPort + 1, 33, 255, -1, -32}),
		newNumericDecoder[LAGID]("LAGID", []int{1, LAGGroupCount}, []int{LAGGroupCount + 1, 255, -1}),
		newNumericDecoder[VLANID]("VLANID", []int{1, 100, int(MaxVLANID)}, []int{int(MaxVLANID) + 1, 4095, 65536, -1}),
		newNumericDecoder[PortVLANID]("PortVLANID", []int{1, 100, MaxPortVLANID}, []int{MaxPortVLANID + 1, 4095, -1}),
		newNumericDecoder[Priority]("Priority", []int{int(PriorityLowest), 2, int(PriorityHighest)}, []int{int(PriorityHighest) + 1, 9, -1}),
		newNumericDecoder[Kbps]("Kbps", []int{int(RateStep), 128, int(MaxBandwidth)}, []int{100, int(RateStep) + 1, int(MaxBandwidth + RateStep), -int(RateStep)}),
	}
}

// seedNumber is what a receiver holds before a decode, to tell "unchanged" from "set": it is
// refused by every Validate, so no successful decode can produce it.
const seedNumber = -1

// guarantee: PortID.UnmarshalText
// guarantee: PortID.UnmarshalJSON
// guarantee: LAGID.UnmarshalText
// guarantee: LAGID.UnmarshalJSON
// guarantee: VLANID.UnmarshalText
// guarantee: VLANID.UnmarshalJSON
// guarantee: PortVLANID.UnmarshalText
// guarantee: PortVLANID.UnmarshalJSON
// guarantee: Priority.UnmarshalText
// guarantee: Priority.UnmarshalJSON
// guarantee: Kbps.UnmarshalText
// guarantee: Kbps.UnmarshalJSON
//
// A decoder reads every number its type's Validate accepts, in the text form, the JSON form, a
// struct field and a map key.
func TestNumericDecodersReadWhatValidateAccepts(t *testing.T) {
	for _, d := range numericDecoders() {
		for _, n := range d.valid {
			in := strconv.Itoa(n)
			for form, decode := range map[string]func(int, string) (int, error){"text": d.text, "JSON": d.jsn, "struct field": d.field} {
				t.Run(d.name+" "+in+" "+form, func(t *testing.T) {
					got, err := decode(seedNumber, in)
					if err != nil || got != n {
						t.Errorf("decoding %s = %d, %v; want %d", in, got, err, n)
					}
				})
			}
			t.Run(d.name+" "+in+" map key", func(t *testing.T) {
				got, err := d.key(in)
				if err != nil || got != n {
					t.Errorf("decoding the key %s = %d, %v; want %d", in, got, err, n)
				}
			})
		}
	}
}

// guarantee: PortID.UnmarshalText
//
// A decoder reads zero as "not set": the number 0, and for the text form the empty text as well; it
// sets the receiver to zero (a field that is absent stays zero, so an explicit 0 behaves alike).
func TestNumericDecodersReadZero(t *testing.T) {
	for _, d := range numericDecoders() {
		for _, tc := range []struct {
			name   string
			decode func(int, string) (int, error)
			in     string
		}{
			{"text 0", d.text, "0"},
			{"text empty", d.text, ""},
			{"JSON 0", d.jsn, "0"},
			{"struct field 0", d.field, "0"},
		} {
			t.Run(d.name+" "+tc.name, func(t *testing.T) {
				got, err := tc.decode(seedNumber, tc.in)
				if err != nil || got != 0 {
					t.Errorf("decoding %q = %d, %v; want 0 and no error", tc.in, got, err)
				}
			})
		}
		t.Run(d.name+" map key 0", func(t *testing.T) {
			if got, err := d.key("0"); err != nil || got != 0 {
				t.Errorf("decoding the key 0 = %d, %v; want 0 and no error", got, err)
			}
		})
	}
}

// guarantee: Kbps.UnmarshalText
// guarantee: Kbps.UnmarshalJSON
//
// A decoder refuses what Validate refuses with Validate's error unchanged (it matches ErrInvalid and
// has the same message) and leaves the receiver as it was.
func TestNumericDecodersRefuseWhatValidateRefuses(t *testing.T) {
	for _, d := range numericDecoders() {
		for _, n := range d.invalid {
			in := strconv.Itoa(n)
			want := d.validate(n)
			if want == nil {
				t.Fatalf("%s: %d is listed as invalid but Validate accepts it", d.name, n)
			}
			for form, decode := range map[string]func(int, string) (int, error){"text": d.text, "JSON": d.jsn, "struct field": d.field} {
				t.Run(d.name+" "+in+" "+form, func(t *testing.T) {
					got, err := decode(seedNumber, in)
					if !errors.Is(err, ErrInvalid) {
						t.Fatalf("decoding %s: got %v, want an error that matches ErrInvalid", in, err)
					}
					if err.Error() != want.Error() {
						t.Errorf("decoding %s: message %q, Validate says %q", in, err, want)
					}
					if got != seedNumber {
						t.Errorf("decoding %s changed the receiver to %d", in, got)
					}
				})
			}
			t.Run(d.name+" "+in+" map key", func(t *testing.T) {
				_, err := d.key(in)
				if !errors.Is(err, ErrInvalid) || err.Error() != want.Error() {
					t.Errorf("decoding the key %s: got %v, want %v", in, err, want)
				}
			})
		}
	}
}

// guarantee: PortID.UnmarshalText
// guarantee: PortID.UnmarshalJSON
//
// The error of a refused value is Validate's, word for word, as the issue that asked for it lists.
func TestNumericDecodersRefuseWithTheMessageOfValidate(t *testing.T) {
	tests := []struct {
		name string
		got  error
		want string
	}{
		{"a port in a record", json.Unmarshal([]byte(`{"Port":33}`), new(PortBandwidth)), "escp: invalid port 33 (valid: 1-32)"},
		{"a PVID in a record", json.Unmarshal([]byte(`{"Port":3,"PVID":4095}`), new(PortPVID)), "escp: invalid VLAN id 4095 (valid: 1-4094)"},
		{"a rate in a record", json.Unmarshal([]byte(`{"Rate":100}`), new(StormConfig)),
			"escp: 100 kbps is not a valid rate: use 0 (unlimited) or a multiple of 64 up to 1000000"},
		{"a map key", json.Unmarshal([]byte(`{"99":"x"}`), new(map[PortID]string)), "escp: invalid port 99 (valid: 1-32)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(tc.got, ErrInvalid) {
				t.Fatalf("got %v, want an error that matches ErrInvalid", tc.got)
			}
			if tc.got.Error() != tc.want {
				t.Errorf("message %q, want %q", tc.got, tc.want)
			}
		})
	}
}

// guarantee: PortID.UnmarshalText
// guarantee: PortID.UnmarshalJSON
//
// A number must be written in its one canonical decimal spelling, in the text form, as a JSON integer
// and inside a JSON string: anything else is refused with ErrInvalid and leaves the receiver as it was.
// A fraction and an exponent were refused by encoding/json before the decoders existed and still are;
// a negative zero was read before and is refused now.
func TestNumericDecodersRefuseOtherSpellings(t *testing.T) {
	huge := strings.Repeat("9", 40)
	texts := []string{"+3", "03", "-0", " 3", "3 ", "3\n", "3.0", "1e1", "0x3", "abc", "null", "٣", huge, "00", "-"}
	jsons := []string{`3.0`, `1e1`, `1E1`, `-0`, `+3`, `03`, `true`, `[1]`, `{}`, ``, ` 3`, `3 `, huge, `0.0`, `-`,
		// A JSON string is read as its text is (before Go 1.27 that is how a map key arrives).
		`"+3"`, `"03"`, `"-0"`, `" 3"`, `"3 "`, `"3.0"`, `"1e1"`, `"0x3"`, `"null"`, `"abc"`, `"\u0663"`, `"` + huge + `"`, `"3`, `"\x"`, `"3"x`}
	for _, d := range numericDecoders() {
		for _, in := range texts {
			t.Run(fmt.Sprintf("%s text %q", d.name, in), func(t *testing.T) {
				got, err := d.text(seedNumber, in)
				if !errors.Is(err, ErrInvalid) || got != seedNumber {
					t.Errorf("got %d, %v; want ErrInvalid and the receiver unchanged", got, err)
				}
			})
			t.Run(fmt.Sprintf("%s map key %q", d.name, in), func(t *testing.T) {
				if _, err := d.key(in); !errors.Is(err, ErrInvalid) {
					t.Errorf("got %v, want ErrInvalid", err)
				}
			})
		}
		for _, in := range jsons {
			t.Run(fmt.Sprintf("%s JSON %q", d.name, in), func(t *testing.T) {
				got, err := d.jsn(seedNumber, in)
				if !errors.Is(err, ErrInvalid) || got != seedNumber {
					t.Errorf("got %d, %v; want ErrInvalid and the receiver unchanged", got, err)
				}
			})
		}
	}
}

// guarantee: PortID.UnmarshalJSON
//
// Through encoding/json a fraction, an exponent, a negative zero and a string that is no canonical
// number are refused in a struct field, and the field keeps its value.
func TestNumericDecodersRefuseOtherSpellingsInAField(t *testing.T) {
	for _, d := range numericDecoders() {
		for _, in := range []string{`3.0`, `1e1`, `-0`, `true`, `[3]`, `"3.0"`, `"+3"`, `"-0"`, `"x"`} {
			t.Run(d.name+" "+in, func(t *testing.T) {
				got, err := d.field(seedNumber, in)
				if err == nil || got != seedNumber {
					t.Errorf("got %d, %v; want an error and the field unchanged", got, err)
				}
			})
		}
	}
}

// guarantee: PortID.UnmarshalJSON
// guarantee: Kbps.UnmarshalJSON
//
// A JSON null is no value: it leaves the receiver as it was and is no error (the convention of
// encoding/json); the text "null" is not a number.
func TestNumericDecodersLeaveTheReceiverAloneForNull(t *testing.T) {
	for _, d := range numericDecoders() {
		t.Run(d.name+" JSON", func(t *testing.T) {
			got, err := d.jsn(seedNumber, "null")
			if err != nil || got != seedNumber {
				t.Errorf("got %d, %v; want the receiver unchanged and no error", got, err)
			}
		})
		t.Run(d.name+" field", func(t *testing.T) {
			got, err := d.field(seedNumber, "null")
			if err != nil || got != seedNumber {
				t.Errorf("got %d, %v; want the field unchanged and no error", got, err)
			}
		})
		t.Run(d.name+" text", func(t *testing.T) {
			if got, err := d.text(seedNumber, "null"); !errors.Is(err, ErrInvalid) || got != seedNumber {
				t.Errorf("got %d, %v; want ErrInvalid and the receiver unchanged", got, err)
			}
		})
	}
}

// guarantee: PortID.UnmarshalText
//
// The encoders did not change: a value is a number as a field and as a map key, and what an encoder
// writes the decoder reads back, for every valid value and for zero.
func TestNumericDecodersReadWhatTheEncodersWrite(t *testing.T) {
	for _, d := range numericDecoders() {
		for _, n := range append([]int{0}, d.valid...) {
			t.Run(fmt.Sprintf("%s %d", d.name, n), func(t *testing.T) {
				field, key, err := d.encode(n)
				if err != nil {
					t.Fatal(err)
				}
				if wantField, wantKey := fmt.Sprintf(`{"V":%d}`, n), fmt.Sprintf(`{"%d":"x"}`, n); field != wantField || key != wantKey {
					t.Errorf("encoded %s and %s; want %s and %s", field, key, wantField, wantKey)
				}
				if got, err := d.field(seedNumber, strconv.Itoa(n)); err != nil || got != n {
					t.Errorf("the field read back as %d, %v", got, err)
				}
				if got, err := d.key(strconv.Itoa(n)); err != nil || got != n {
					t.Errorf("the key read back as %d, %v", got, err)
				}
			})
		}
	}
}

// guarantee: PortID.UnmarshalText
//
// A refusal of a text that is no number quotes at most maxEcho bytes of it, so that a huge value does
// not make a huge error; a text of exactly maxEcho bytes is quoted whole.
func TestNumericDecodersBoundTheEchoOfAText(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantShort bool
	}{
		{"as long as the limit", strings.Repeat("x", maxEcho), false},
		{"one byte longer", strings.Repeat("x", maxEcho+1), true},
		{"a megabyte", strings.Repeat("x", 1<<20), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var p PortID
			err := p.UnmarshalText([]byte(tc.text))
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("got %v, want ErrInvalid", err)
			}
			if got := strings.Contains(err.Error(), fmt.Sprintf("(%d bytes)", len(tc.text))); got != tc.wantShort {
				t.Errorf("the error mentions the length: %v, want %v: %.120s", got, tc.wantShort, err)
			}
			if tc.wantShort && len(err.Error()) > 200 {
				t.Errorf("the error is %d bytes long", len(err.Error()))
			}
			if !tc.wantShort && !strings.Contains(err.Error(), tc.text) {
				t.Errorf("the error does not quote the text: %s", err)
			}
		})
	}
}

// ParsePortID and the text decoder of PortID read the same spelling and say the same of what is no
// number: one parser (canonicalInt), one message.
func TestParsePortIDAndTheDecoderAgreeOnSpelling(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"a plus sign", "+3"},
		{"a leading zero", "03"},
		{"a word", "port"},
		{"a negative zero", "-0"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, perr := ParsePortID(tc.in)
			var p PortID
			derr := p.UnmarshalText([]byte(tc.in))
			if perr == nil || derr == nil || perr.Error() != derr.Error() {
				t.Errorf("ParsePortID says %v, the decoder says %v", perr, derr)
			}
			if want := fmt.Sprintf("escp: %q is not a port number", tc.in); perr.Error() != want {
				t.Errorf("message %q, want %q", perr, want)
			}
		})
	}
}

// guarantee: PortID.UnmarshalJSON
// guarantee: Kbps.UnmarshalJSON
//
// A JSON string is read as its content is by the text decoder, in a struct field and directly: the
// canonical number reads, the empty string and "0" are zero, and a number Validate refuses is refused
// with Validate's error and leaves the receiver as it was. (Before Go 1.27 encoding/json hands the key
// of a map to UnmarshalJSON as a quoted string, so a key and a quoted field cannot be told apart.)
func TestNumericDecodersReadAJSONString(t *testing.T) {
	for _, d := range numericDecoders() {
		forms := map[string]func(int, string) (int, error){"JSON": d.jsn, "struct field": d.field}
		for form, decode := range forms {
			for _, n := range append([]int{0}, d.valid...) {
				in := `"` + strconv.Itoa(n) + `"`
				t.Run(fmt.Sprintf("%s %s %s", d.name, form, in), func(t *testing.T) {
					if got, err := decode(seedNumber, in); err != nil || got != n {
						t.Errorf("decoding %s = %d, %v; want %d", in, got, err, n)
					}
				})
			}
			t.Run(d.name+" "+form+" the empty string", func(t *testing.T) {
				if got, err := decode(seedNumber, `""`); err != nil || got != 0 {
					t.Errorf("decoding the empty string = %d, %v; want 0", got, err)
				}
			})
			for _, n := range d.invalid {
				in := `"` + strconv.Itoa(n) + `"`
				t.Run(fmt.Sprintf("%s %s %s", d.name, form, in), func(t *testing.T) {
					got, err := decode(seedNumber, in)
					if want := d.validate(n); !errors.Is(err, ErrInvalid) || err.Error() != want.Error() || got != seedNumber {
						t.Errorf("decoding %s = %d, %v; want %v and the receiver unchanged", in, got, err, want)
					}
				})
			}
		}
	}
	t.Run("an escaped digit is the digit", func(t *testing.T) {
		var p PortID
		if err := p.UnmarshalJSON([]byte(`"3"`)); err != nil || p != 3 {
			t.Errorf("got %d, %v; want 3", p, err)
		}
	})
}
