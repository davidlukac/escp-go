package readiness

import (
	"fmt"
	"io"
	"strings"
	"unicode"
)

// BodyVariable is the environment variable that holds the pull request description.
const BodyVariable = "PR_BODY"

// Limits of the output.
const (
	// maxListed is the most boxes a verdict lists.
	maxListed = 50
	// maxLine is the longest text of a box that is printed, in characters.
	maxLine = 200
	// ellipsis ends a text that was cut.
	ellipsis = "..."
	// boxPrefix starts every line that names a box.
	boxPrefix = "  - [ ] "
)

// Messages of the verdicts that have no list.
const (
	msgNoSection = "no merge readiness section: nothing to check"
	msgAllTicked = "every box of the merge readiness section is ticked"
	msgNoBoxes   = "merge readiness: the section has no boxes: restore the boxes of the template or delete the section"
)

// Verdict is the outcome of the check for one description.
type Verdict struct {
	// Pass is true when the pull request may pass the check.
	Pass bool
	// Unticked is the number of boxes the verdict names, listed or not.
	Unticked int
	// Lines is the output, without line ends, in the order it is printed.
	Lines []string
}

// Judge reads the description and returns the verdict.
func Judge(body string) Verdict {
	lines := splitLines(body)
	structured := structuredPass(lines)
	found, naive := naivePass(lines)
	switch {
	case len(naive) > len(structured.unticked):
		heading := fmt.Sprintf("merge readiness: ambiguous markup: %d possible box(es) not ticked, %d recognized:",
			len(naive), len(structured.unticked)+structured.ticked)
		return red(heading, naive)
	case len(structured.unticked) > 0:
		return red(fmt.Sprintf("merge readiness: %d box(es) not ticked:", len(structured.unticked)), structured.unticked)
	case structured.sections == 0 && !found:
		return Verdict{Pass: true, Lines: []string{msgNoSection}}
	case structured.ticked == 0:
		return Verdict{Lines: []string{msgNoBoxes}}
	default:
		return Verdict{Pass: true, Lines: []string{msgAllTicked}}
	}
}

// red is the verdict that lists boxes, at most maxListed of them. Every box is printed after the fixed
// prefix, so a line of the output never starts with the text of a box, and its characters are made
// printable.
func red(heading string, boxes []string) Verdict {
	lines := []string{heading}
	for _, text := range boxes[:min(len(boxes), maxListed)] {
		lines = append(lines, boxPrefix+printable(text))
	}
	if len(boxes) > maxListed {
		lines = append(lines, fmt.Sprintf("... and %d more", len(boxes)-maxListed))
	}
	return Verdict{Unticked: len(boxes), Lines: lines}
}

// printable is the text with each control or other unprintable character replaced with "?", and cut
// to maxLine characters.
func printable(text string) string {
	runes := []rune(text)
	for i, r := range runes {
		if !unicode.IsPrint(r) {
			runes[i] = '?'
		}
	}
	if len(runes) > maxLine {
		return string(runes[:maxLine-len(ellipsis)]) + ellipsis
	}
	return string(runes)
}

// Exit codes of Run.
const (
	exitPass = 0
	exitRed  = 1
)

// Run is the whole command: it judges the description in the variable BodyVariable of the environment
// getenv reads (an unset variable is an empty description), prints the verdict to out and returns the
// exit status: 0 when the pull request passes, 1 when it does not or when the verdict could not be written.
func Run(getenv func(string) string, out io.Writer) int {
	verdict := Judge(getenv(BodyVariable))
	text := strings.Join(verdict.Lines, "\n") + "\n"
	if _, err := io.WriteString(out, text); err != nil || !verdict.Pass {
		return exitRed
	}
	return exitPass
}
