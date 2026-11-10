package safety

import (
	"regexp"
	"strings"
	"testing"

	"github.com/davidlukac/escp-go/model"
)

// buildDateShape is what a build date is, written apart from the parser's own pattern.
var buildDateShape = regexp.MustCompile(`^[0-9]{8}$`)

// FuzzParseCompatibility: the compatibility table is read from a JSON document (embedded today, and
// the firmware gate trusts what it reads), so no document makes the parser panic, and a table it
// accepts holds what its documentation promises: known support levels, the three names of a row,
// builds that are build dates (or empty) and appear once, and builds on every verified row. The rows are no
// more than the document can hold, and every accepted table renders.
func FuzzParseCompatibility(f *testing.F) {
	f.Add(compatibilityJSON)
	f.Add([]byte(`{"rows":[]}`))
	f.Add([]byte(`{"rows":[{"model":"M","hardware":"v1","firmware":"1.0","support":"untried"}]}`))
	f.Add([]byte(`{"rows":[{"model":"M","hardware":"v1","firmware":"1.0","support":"verified","builds":["20230218"]}]}`))
	f.Add([]byte(`{"rows":[{"model":"M","hardware":"v1","firmware":"1.0","support":"verified","builds":["20230218","20230218"]}]}`))
	f.Add([]byte(`{"rows":[{"model":"M","hardware":"v1","firmware":"1.0","support":"verified","builds":[""]}]}`))
	f.Add([]byte(`{"rows":[{}]}`))
	f.Add([]byte(`{"rows":null}`))
	f.Add([]byte(`[`))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		rows, err := parseCompatibility(data)
		if err != nil {
			if rows != nil {
				t.Fatalf("a refused table returned %d rows", len(rows))
			}
			return
		}
		if len(rows) > len(data) {
			t.Fatalf("%d rows from %d bytes", len(rows), len(data))
		}
		seen := map[model.BuildDate]bool{}
		for i, r := range rows {
			switch r.Support {
			case SupportVerified, SupportUntried, SupportUntested, SupportUnsupported:
			default:
				t.Fatalf("row %d: support %q accepted", i+1, r.Support)
			}
			if r.Model == "" || r.Hardware == "" || r.Firmware == "" {
				t.Fatalf("row %d accepted without a model, hardware or firmware: %+v", i+1, r)
			}
			if r.Support == SupportVerified && len(r.Builds) == 0 {
				t.Fatalf("row %d is verified and names no build", i+1)
			}
			for _, b := range r.Builds {
				// The empty text decodes as "no build date" (BuildDate.UnmarshalText), so an empty entry is
				// accepted today; it never matches, because the gate asks Firmware.HasBuild first.
				if (b != "" && !buildDateShape.MatchString(string(b))) || seen[b] {
					t.Fatalf("row %d: build %q is not a build date or appears twice", i+1, b)
				}
				seen[b] = true
			}
		}
		// A header of two lines, then one line per row (a summary with a newline in it adds more).
		if table := renderCompatibility(rows); strings.Count(table, "\n") < len(rows)+2 || !strings.HasSuffix(table, "\n") {
			t.Fatalf("the table of %d rows renders as %q", len(rows), table)
		}
	})
}
