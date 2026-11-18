package readiness

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode"
)

func TestTheUntickedBoxesAreListedInOrder(t *testing.T) {
	got := Judge(section)
	want := []string{
		"merge readiness: 4 box(es) not ticked:",
		"  - [ ] CI is green on the head commit",
		"  - [ ] Local checks done",
		"  - [ ] **Ready for the maintainer to merge**",
		"  - [ ] a task (reviewer)",
	}
	if strings.Join(got.Lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got.Lines, "\n"), strings.Join(want, "\n"))
	}
}

func TestAnEmptyBoxIsListedWithTheFullPrefix(t *testing.T) {
	if got := Judge(heading + "- [ ]\n"); got.Lines[1] != "  - [ ] " {
		t.Errorf("got %q", got.Lines[1])
	}
}

// The runner trims the leading blanks of a line before it looks for a workflow command, so what keeps a
// box's text from becoming one is the fixed prefix of the line, and what keeps a control character out
// is the sanitizing.
func TestNoLineOfTheOutputIsAWorkflowCommandOrHoldsAControlCharacter(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"a command", "::error::boom"},
		{"a command after blanks", "   ::add-mask::x"},
		{"an escape sequence", "\x1b[31mred"},
		{"a bell and a delete", "a\x07b\x7fc"},
		{"a line separator", "a\u2028::error::b"},
		{"a next line", "a\u0085::error::b"},
		{"a bidirectional override", "a\u202eb"},
		{"an invalid byte", "a\xffb"},
		{"a vertical tab", "a\vb"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Judge(heading + "- [ ] " + tt.text + "\n")
			if got.Pass || len(got.Lines) != 2 {
				t.Fatalf("Judge = %v %q", got.Pass, got.Lines)
			}
			for _, l := range got.Lines {
				if strings.HasPrefix(strings.TrimLeftFunc(l, unicode.IsSpace), "::") {
					t.Errorf("a workflow command: %q", l)
				}
				if strings.IndexFunc(l, func(r rune) bool { return unicode.IsControl(r) || r == '\u2028' || r == '\u202e' }) >= 0 {
					t.Errorf("a control character: %q", l)
				}
			}
		})
	}
}

func TestALongTextIsCutAtTheLimit(t *testing.T) {
	tests := []struct {
		name string
		n    int
		want string
	}{
		{"one under", 199, strings.Repeat("a", 199)},
		{"at the limit", 200, strings.Repeat("a", 200)},
		{"one over", 201, strings.Repeat("a", 197) + "..."},
		{"far over", 5000, strings.Repeat("a", 197) + "..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Judge(heading + "- [ ] " + strings.Repeat("a", tt.n) + "\n"); got.Lines[1] != boxPrefix+tt.want {
				t.Errorf("got %d characters: %q", len(got.Lines[1]), got.Lines[1])
			}
		})
	}
}

func TestACutCountsCharactersNotBytes(t *testing.T) {
	text := strings.Repeat("\u00e9", 201)
	want := boxPrefix + strings.Repeat("\u00e9", 197) + "..."
	if got := Judge(heading + "- [ ] " + text + "\n"); got.Lines[1] != want {
		t.Errorf("got %q", got.Lines[1])
	}
}

func TestTheListIsCutAtFiftyBoxesAndTheRestIsCounted(t *testing.T) {
	tests := []struct {
		boxes int
		lines int
		last  string
	}{
		{49, 50, "  - [ ] box"},
		{50, 51, "  - [ ] box"},
		{51, 52, "... and 1 more"},
		{57, 52, "... and 7 more"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.boxes), func(t *testing.T) {
			got := Judge(heading + strings.Repeat("- [ ] box\n", tt.boxes))
			if got.Unticked != tt.boxes || len(got.Lines) != tt.lines || got.Lines[len(got.Lines)-1] != tt.last {
				t.Errorf("unticked %d, %d lines, last %q", got.Unticked, len(got.Lines), got.Lines[len(got.Lines)-1])
			}
			if want := fmt.Sprintf("merge readiness: %d box(es) not ticked:", tt.boxes); got.Lines[0] != want {
				t.Errorf("heading %q, want %q", got.Lines[0], want)
			}
		})
	}
}

// A scan that backtracked would take minutes on these; the bound is far above what a linear scan needs
// and far below what a quadratic one takes.
func TestHostileLinesAreReadInMomentsWhateverTheyHold(t *testing.T) {
	const bound = 5 * time.Second
	big := 65000
	tests := []struct {
		name string
		body string
	}{
		{"spaces inside a heading", "## a" + strings.Repeat(" ", big) + "b\n" + heading + "- [ ] x\n"},
		{"tabs inside a heading", "## a" + strings.Repeat("\t", big) + "b\n" + heading + "- [ ] x\n"},
		{"spaces and a closing sequence", "## a" + strings.Repeat(" #", big/2) + "\n"},
		{"spaces then hashes in the section heading", "## Merge readiness" + strings.Repeat(" ", big) + "#\n- [ ] x\n"},
		{"a long run of markers", heading + strings.Repeat("- ", big/2) + "[ ] x\n"},
		{"a long run of quotes and blanks", heading + strings.Repeat("> ", big/2) + "- [ ] x\n"},
		{"a long run of digits", heading + strings.Repeat("1", big) + ". [ ] x\n"},
		{"many comments on a line", heading + strings.Repeat("<!--a-->", big/8) + "\n- [x] y\n"},
		{"many unclosed comment openers", heading + strings.Repeat("<!--", big/4) + "\n- [x] y\n"},
		{"many fence lines", heading + strings.Repeat("```\n", big) + "- [x] y\n"},
		{"many lines of the filler", strings.Repeat("text with [ ] and <!-- a comment --> and ``` and ## inside a line\n", 20000) + section},
		{"long lines of backticks", heading + strings.Repeat("`", big) + " a`\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			started := time.Now()
			got := Judge(tt.body)
			if elapsed := time.Since(started); elapsed > bound {
				t.Errorf("took %v for %d characters", elapsed, len(tt.body))
			}
			if len(got.Lines) == 0 {
				t.Error("no verdict")
			}
		})
	}
}

func TestALargeBodyIsStillJudgedRight(t *testing.T) {
	filler := strings.Repeat("text with [ ] and <!-- a comment --> and ``` and ## inside a line\n", 20000)
	if got := Judge(filler + section + filler); got.Pass || got.Unticked != 4 {
		t.Errorf("pass %v, unticked %d (%q)", got.Pass, got.Unticked, got.Lines)
	}
}

// failingWriter refuses every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestRunPrintsTheVerdictAndReturnsTheExitStatus(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		code int
		out  string
	}{
		{"an unset variable passes", nil, 0, msgNoSection + "\n"},
		{"an empty variable passes", map[string]string{BodyVariable: ""}, 0, msgNoSection + "\n"},
		{"a ticked description passes", map[string]string{BodyVariable: ticked}, 0, msgAllTicked + "\n"},
		{"an unticked box fails", map[string]string{BodyVariable: heading + "- [ ] a\n"}, 1, "merge readiness: 1 box(es) not ticked:\n  - [ ] a\n"},
		{"only the variable is read", map[string]string{"OTHER": section, "pr_body": section}, 0, msgNoSection + "\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			if code := Run(func(name string) string { return tt.env[name] }, &out); code != tt.code || out.String() != tt.out {
				t.Errorf("code %d, output %q; want %d, %q", code, out.String(), tt.code, tt.out)
			}
		})
	}
}

func TestRunFailsWhenTheVerdictCannotBeWritten(t *testing.T) {
	if code := Run(func(string) string { return ticked }, failingWriter{}); code != exitRed {
		t.Errorf("code %d, want %d", code, exitRed)
	}
}

func TestRunAsksForNothingButTheDescription(t *testing.T) {
	var asked []string
	Run(func(name string) string { asked = append(asked, name); return "" }, &strings.Builder{})
	if len(asked) != 1 || asked[0] != BodyVariable {
		t.Errorf("asked for %q", asked)
	}
}
