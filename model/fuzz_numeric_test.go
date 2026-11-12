package model

import (
	"encoding/json"
	"errors"
	"strconv"
	"testing"
)

// FuzzNumericDecoders: no bytes make a decoder of a numeric input type panic; the text decoder
// accepts exactly the empty text and the canonical decimal numbers that are zero or that Validate
// accepts, the JSON decoder the same without the empty text and with null, which leaves the receiver
// alone; a refusal matches ErrInvalid and leaves the receiver as it was; a number that was read is
// read again from the text an encoder writes for it (DC-L05, DC-L06, escp-go#216).
func FuzzNumericDecoders(f *testing.F) {
	for _, s := range []string{
		"", "0", "1", "3", "8", "32", "33", "64", "100", "128", "255", "256", "4094", "4095", "1000000", "1000064",
		"-1", "-0", "+3", "03", "3.0", "1e1", "0x3", `"3"`, "null", "true", "[1]", "{}", " 3", "3\n", "٣", "\x00",
		"9223372036854775807", "9223372036854775808", "99999999999999999999999",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		checkNumericDecoders[PortID](t, "PortID", data)
		checkNumericDecoders[LAGID](t, "LAGID", data)
		checkNumericDecoders[VLANID](t, "VLANID", data)
		checkNumericDecoders[PortVLANID](t, "PortVLANID", data)
		checkNumericDecoders[Priority](t, "Priority", data)
		checkNumericDecoders[Kbps](t, "Kbps", data)
	})
}

// wantNumber is the independent oracle of the fuzz target: whether data is a canonical decimal number
// that is zero or that validate accepts, and which one.
func wantNumber(data []byte, validate func(int) error) (int, bool) {
	n, err := strconv.Atoi(string(data))
	if err != nil || strconv.Itoa(n) != string(data) {
		return 0, false
	}
	return n, n == 0 || validate(n) == nil
}

func checkNumericDecoders[T numeric, P numericPtr[T]](t *testing.T, name string, data []byte) {
	t.Helper()
	validate := func(n int) error { return T(n).Validate() }
	n, accepted := wantNumber(data, validate)

	v := T(seedNumber)
	err := P(&v).UnmarshalText(data)
	switch {
	case len(data) == 0 && (err != nil || v != 0):
		t.Fatalf("%s: UnmarshalText of the empty text gave %d, %v", name, v, err)
	case len(data) > 0 && accepted && (err != nil || int(v) != n):
		t.Fatalf("%s: UnmarshalText(%q) gave %d, %v; want %d", name, data, v, err, n)
	case len(data) > 0 && !accepted && (!errors.Is(err, ErrInvalid) || int(v) != seedNumber):
		t.Fatalf("%s: UnmarshalText(%q) gave %d, %v; want ErrInvalid and the receiver unchanged", name, data, v, err)
	}

	v = T(seedNumber)
	err = P(&v).UnmarshalJSON(data)
	switch {
	case string(data) == "null" && (err != nil || int(v) != seedNumber):
		t.Fatalf("%s: UnmarshalJSON(null) gave %d, %v; want the receiver unchanged", name, v, err)
	case string(data) != "null" && accepted && (err != nil || int(v) != n):
		t.Fatalf("%s: UnmarshalJSON(%q) gave %d, %v; want %d", name, data, v, err, n)
	case string(data) != "null" && !accepted && (!errors.Is(err, ErrInvalid) || int(v) != seedNumber):
		t.Fatalf("%s: UnmarshalJSON(%q) gave %d, %v; want ErrInvalid and the receiver unchanged", name, data, v, err)
	}

	if !accepted {
		return
	}
	// What was read is read again from what the encoders write for it.
	encoded, err := json.Marshal(T(n))
	if err != nil || string(encoded) != strconv.Itoa(n) {
		t.Fatalf("%s: %d encodes as %s, %v", name, n, encoded, err)
	}
	var again T
	if err := P(&again).UnmarshalJSON(encoded); err != nil || int(again) != n {
		t.Fatalf("%s: %s reads back as %d, %v", name, encoded, again, err)
	}
	var viaText T
	if err := P(&viaText).UnmarshalText([]byte(strconv.Itoa(n))); err != nil || int(viaText) != n {
		t.Fatalf("%s: %d reads back from its text as %d, %v", name, n, viaText, err)
	}
}
