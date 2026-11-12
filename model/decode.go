package model

import (
	"strconv"
)

// maxEcho is the most bytes of a refused text that an error quotes, so that a huge value (a map key,
// a field of a file) does not become a huge error message.
const maxEcho = 40

// canonicalInt reads s as a number in its one canonical decimal spelling: digits only, with a minus
// sign for a negative number, without a plus sign, a leading zero, a space or an exponent ("3", "-1",
// "0", but not "+3", "03", "-0", " 3" or "3.0"), so that one number has one text. It is the one
// reader of numbers in this package: ParsePortID, the text decoders and the JSON decoders of the
// numeric types all take their syntax from it.
func canonicalInt(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	if err != nil || strconv.Itoa(n) != s {
		return 0, false
	}
	return n, true
}

// notANumber is the error of a text that is no number in its canonical spelling; what names the
// kind of number, with its article ("a port number"). It matches ErrInvalid and quotes at most
// maxEcho bytes of the text.
func notANumber(s, what string) error {
	if len(s) > maxEcho {
		return invalidf("escp: %q... (%d bytes) is not %s", s[:maxEcho], len(s), what)
	}
	return invalidf("escp: %q is not %s", s, what)
}

// numeric is an integer input type: a number that has a Validate method, which is its one rule.
type numeric interface {
	~int
	Validate() error
}

// readNumber is the rule of the decoders of the numeric types: the zero value is read as "not
// set", any other number must pass the type's own Validate, whose error is returned unchanged. The
// receiver is assigned only when the number is read.
func readNumber[T numeric](dst *T, n int) error {
	v := T(n)
	if n != 0 {
		if err := v.Validate(); err != nil {
			return err
		}
	}
	*dst = v
	return nil
}

// readNumberText reads the text form of a numeric type: the empty text is the zero value, any
// other text is a canonical decimal number (canonicalInt) that readNumber then applies the rule to.
func readNumberText[T numeric](dst *T, text []byte, what string) error {
	if len(text) == 0 {
		*dst = 0
		return nil
	}
	n, ok := canonicalInt(string(text))
	if !ok {
		return notANumber(string(text), what)
	}
	return readNumber(dst, n)
}

// readNumberJSON reads the JSON form of a numeric type: null leaves the receiver alone (the
// convention of encoding/json), any other value must be an integer literal (canonicalInt), so that a
// quoted number, a fraction, an exponent and a negative zero are refused, and readNumber then
// applies the rule to it.
func readNumberJSON[T numeric](dst *T, data []byte, what string) error {
	if string(data) == "null" {
		return nil
	}
	n, ok := canonicalInt(string(data))
	if !ok {
		return notANumber(string(data), what)
	}
	return readNumber(dst, n)
}

// The decoders of the numeric input types. Each reads a number the way the type's own Validate
// rules, so that decoding a file or a snapshot refuses what a writer would refuse, with the same
// error (it matches ErrInvalid and has Validate's message), and also reads 0, which is how a
// record says "not set" (a port that is in no LAG, a mirror without a monitoring port, no rate
// limit); whether "not set" is allowed where the value is used stays with Validate and the writers.
// A value that is refused leaves the receiver as it was, and a JSON null does not touch it. The two
// forms are both needed: encoding/json hands a JSON number to UnmarshalJSON and the text of a map
// key to UnmarshalText. The encoders did not change, a value still encodes as its number.

// UnmarshalText reads the port number written in decimal (the spelling ParsePortID takes), or the
// empty text, or "0" for no port; a port that is not valid is refused with Validate's error. It is
// the decoder of a map key.
func (p *PortID) UnmarshalText(text []byte) error {
	return readNumberText(p, text, "a port number")
}

// UnmarshalJSON reads a JSON integer for a port with the rule of UnmarshalText; null leaves p alone.
func (p *PortID) UnmarshalJSON(data []byte) error {
	return readNumberJSON(p, data, "a port number")
}

// UnmarshalText reads a LAG group number written in decimal, or the empty text, or "0" for no
// group; a group that is not valid is refused with Validate's error. It is the decoder of a map key.
func (g *LAGID) UnmarshalText(text []byte) error {
	return readNumberText(g, text, "a LAG group number")
}

// UnmarshalJSON reads a JSON integer for a LAG group with the rule of UnmarshalText; null leaves g
// alone.
func (g *LAGID) UnmarshalJSON(data []byte) error {
	return readNumberJSON(g, data, "a LAG group number")
}

// UnmarshalText reads an 802.1Q VLAN id written in decimal, or the empty text, or "0" for no VLAN;
// an id that is not valid is refused with Validate's error. It is the decoder of a map key.
func (v *VLANID) UnmarshalText(text []byte) error {
	return readNumberText(v, text, "a VLAN id")
}

// UnmarshalJSON reads a JSON integer for a VLAN id with the rule of UnmarshalText; null leaves v
// alone.
func (v *VLANID) UnmarshalJSON(data []byte) error {
	return readNumberJSON(v, data, "a VLAN id")
}

// UnmarshalText reads a port-based VLAN id written in decimal, or the empty text, or "0" for no
// group; an id that is not valid is refused with Validate's error. It is the decoder of a map key.
func (g *PortVLANID) UnmarshalText(text []byte) error {
	return readNumberText(g, text, "a port-based VLAN id")
}

// UnmarshalJSON reads a JSON integer for a port-based VLAN id with the rule of UnmarshalText; null
// leaves g alone.
func (g *PortVLANID) UnmarshalJSON(data []byte) error {
	return readNumberJSON(g, data, "a port-based VLAN id")
}

// UnmarshalText reads a priority written in decimal, or the empty text, or "0" for none; a priority
// that is not valid is refused with Validate's error. It is the decoder of a map key.
func (p *Priority) UnmarshalText(text []byte) error {
	return readNumberText(p, text, "a priority")
}

// UnmarshalJSON reads a JSON integer for a priority with the rule of UnmarshalText; null leaves p
// alone.
func (p *Priority) UnmarshalJSON(data []byte) error {
	return readNumberJSON(p, data, "a priority")
}

// UnmarshalText reads a rate in kilobits per second written in decimal, or the empty text, or "0"
// for unlimited; a rate that is not valid is refused with Validate's error. It is the decoder of a
// map key.
func (k *Kbps) UnmarshalText(text []byte) error {
	return readNumberText(k, text, "a rate in kbps")
}

// UnmarshalJSON reads a JSON integer for a rate with the rule of UnmarshalText; null leaves k alone.
func (k *Kbps) UnmarshalJSON(data []byte) error {
	return readNumberJSON(k, data, "a rate in kbps")
}
