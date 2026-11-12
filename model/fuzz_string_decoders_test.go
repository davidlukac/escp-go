package model

import (
	"encoding/json"
	"errors"
	"testing"
)

// maxFuzzedMessage bounds the error of a refusal in the fuzz target below: 40 quoted bytes, each at
// worst four characters of escape, and the fixed text of the two messages.
const maxFuzzedMessage = 400

// FuzzDecodeStrings: no text makes the decoder of Hostname or VLANName panic; the decoder accepts a
// text exactly when Validate accepts it or the text is empty; a refusal is Validate's own error, matches
// ErrInvalid, stays short however long the text is, and leaves the receiver alone; an accepted value
// reads back as the text and survives a JSON round trip unchanged.
func FuzzDecodeStrings(f *testing.F) {
	for _, s := range []string{
		"", "sw-1", "a", "-a", "a-", "a b", "bad name!", "a.b", "a_b", "café", "a\x00b", "a\nb", "\xff", "a\u202eb",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "aaaaaaaaaa", "aaaaaaaaaaa",
		"0123456789012345678901234567890123456789éé", "\"", "\\", "null",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, d := range stringDecoders() {
			validateErr := d.validate(s)
			got, err := d.decode(d.keep, s)
			if (err == nil) != (validateErr == nil || s == "") {
				t.Fatalf("%s: decoding %q gives %v, Validate gives %v", d.name, s, err, validateErr)
			}
			if err != nil {
				if !errors.Is(err, ErrInvalid) || err.Error() != validateErr.Error() || len(err.Error()) > maxFuzzedMessage {
					t.Fatalf("%s: the refusal of %q is %q, want Validate's %q, matching ErrInvalid, in at most %d bytes", d.name, s, err, validateErr, maxFuzzedMessage)
				}
				if got != d.keep {
					t.Fatalf("%s: the receiver is %q after the refusal of %q", d.name, got, s)
				}
				continue
			}
			if got != s {
				t.Fatalf("%s: decoding %q gives %q", d.name, s, got)
			}
			text, merr := json.Marshal(s)
			if merr != nil {
				continue // not valid UTF-8: no JSON string to round trip
			}
			if back, jerr := d.decodeJSON(text); jerr != nil || back != s {
				t.Fatalf("%s: %q does not survive JSON as %s: got %q, %v", d.name, s, text, back, jerr)
			}
		}
	})
}
