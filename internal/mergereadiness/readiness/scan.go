package readiness

import (
	"slices"
	"strings"
	"unicode"
)

// Limits and markers of the Markdown the scans read.
const (
	// maxIndent is the most spaces a heading or a fence may have before it; four make an indented code block.
	maxIndent = 3
	// maxHeadingLevel is the deepest ATX heading.
	maxHeadingLevel = 6
	// minFence is the shortest run of backticks or tildes that opens a fence.
	minFence = 3
	// maxMarkerDigits is the longest number of an ordered list marker.
	maxMarkerDigits = 9
	// taskBoxLen is the length of "[ ]".
	taskBoxLen = 3

	commentOpen  = "<!--"
	commentClose = "-->"

	bullets    = "-*+"
	fenceChars = "`~"
	quote      = '>'
)

// line is one line of the description as runes: the structural characters are ASCII, but a no-break
// space or another space in a box is a character of its own.
type line []rune

// isBlank is a space, a tab or a no-break space.
func isBlank(r rune) bool { return r == ' ' || r == '\t' || r == '\u00a0' }

// isSpaceOrTab is a space or a tab: what separates a marker from its text in the strict reading.
func isSpaceOrTab(r rune) bool { return r == ' ' || r == '\t' }

// countIndent is the number of spaces at the start of l, counted up to limit.
func countIndent(l line, limit int) int {
	count := 0
	for count < limit && count < len(l) && l[count] == ' ' {
		count++
	}
	return count
}

// skip returns the position of the first rune at or after pos for which keep is false.
func skip(l line, pos int, keep func(rune) bool) int {
	for pos < len(l) && keep(l[pos]) {
		pos++
	}
	return pos
}

// hasAt reports whether l has the ASCII text s at pos.
func hasAt(l line, pos int, s string) bool {
	for i := range len(s) {
		if pos+i >= len(l) || l[pos+i] != rune(s[i]) {
			return false
		}
	}
	return true
}

// indexFrom is the position of the first occurrence of s in l at or after from, or -1.
func indexFrom(l line, from int, s string) int {
	for pos := from; pos < len(l); pos++ {
		if hasAt(l, pos, s) {
			return pos
		}
	}
	return -1
}

// headingOf returns the level and the normalized title of an ATX heading line, and whether l is one.
func headingOf(l line) (level int, title string, ok bool) {
	start := countIndent(l, maxIndent+1)
	if start > maxIndent {
		return 0, "", false
	}
	level = skip(l, start, func(r rune) bool { return r == '#' }) - start
	if level == 0 || level > maxHeadingLevel {
		return 0, "", false
	}
	rest := l[start+level:]
	if len(rest) > 0 && !isSpaceOrTab(rest[0]) {
		return 0, "", false
	}
	return level, titleOf(rest), true
}

// titleOf is the title of a heading from the text after its marker: its closing sequence of # cut, its
// whitespace collapsed and its case folded.
func titleOf(rest line) string {
	title := strings.Trim(string(rest), " \t")
	if strings.HasSuffix(title, "#") {
		stripped := strings.TrimRight(title, "#")
		switch {
		case stripped == "":
			title = ""
		case stripped[len(stripped)-1] == ' ' || stripped[len(stripped)-1] == '\t':
			title = stripped
		}
	}
	return strings.ToLower(strings.Join(strings.Fields(title), " "))
}

// fenceMarker is the opening fence of l (its run of backticks or tildes), or nil when l opens none.
func fenceMarker(l line) line {
	start := countIndent(l, maxIndent+1)
	if start > maxIndent || start >= len(l) || !strings.ContainsRune(fenceChars, l[start]) {
		return nil
	}
	char := l[start]
	end := skip(l, start, func(r rune) bool { return r == char })
	if end-start < minFence {
		return nil
	}
	// The info string of a backtick fence has no backtick.
	if char == '`' && slices.Contains(l[end:], '`') {
		return nil
	}
	return l[start:end]
}

// closesFence reports whether l closes the fence: the same character, at least as long, then only blanks.
func closesFence(l, fence line) bool {
	marker := fenceMarker(l)
	if marker == nil || marker[0] != fence[0] || len(marker) < len(fence) {
		return false
	}
	start := countIndent(l, maxIndent)
	rest := l[start+len(marker):]
	return skip(rest, 0, isSpaceOrTab) == len(rest)
}

// strictBox reports whether l is a list item that starts with a box, and which: ticked or not, and its
// text. The marker is -, * or + followed by a space or a tab; the box is [ ] (unticked) or [x] or [X]
// (ticked), followed by the end of the line or a space or a tab.
func strictBox(l line) (unticked bool, text string, ok bool) {
	pos := skip(l, 0, isSpaceOrTab)
	if pos >= len(l) || !strings.ContainsRune(bullets, l[pos]) {
		return false, "", false
	}
	pos++
	if pos >= len(l) || !isSpaceOrTab(l[pos]) {
		return false, "", false
	}
	pos = skip(l, pos, isSpaceOrTab)
	if len(l) < pos+taskBoxLen || l[pos] != '[' || l[pos+2] != ']' {
		return false, "", false
	}
	tail := l[pos+taskBoxLen:]
	if len(tail) > 0 && !isSpaceOrTab(tail[0]) {
		return false, "", false
	}
	text = strings.Trim(string(tail), " \t")
	switch l[pos+1] {
	case ' ':
		return true, text, true
	case 'x', 'X':
		return false, text, true
	default:
		return false, "", false
	}
}

// listMarkerEnd is the end of the list marker at pos (a bullet, or digits and a . or )), or pos when
// there is none.
func listMarkerEnd(l line, pos int) int {
	if pos < len(l) && strings.ContainsRune(bullets, l[pos]) {
		return pos + 1
	}
	end := pos
	for end < len(l) && end-pos < maxMarkerDigits && unicode.IsDigit(l[end]) {
		end++
	}
	if end > pos && end < len(l) && (l[end] == '.' || l[end] == ')') {
		return end + 1
	}
	return pos
}

// skipBlanksAndQuotes is the position after the blanks and the > of a quote at or after pos.
func skipBlanksAndQuotes(l line, pos int) int {
	return skip(l, pos, func(r rune) bool { return isBlank(r) || r == quote })
}

// naiveBox returns the text of the box and true when l has the shape of an unticked task list item,
// whatever is in front of it: a quote prefix, any list marker (bullets, numbers), nested markers, tabs
// and no-break spaces. At least one marker and a blank after each, then "[", one whitespace
// character and "]".
func naiveBox(l line) (string, bool) {
	pos := skipBlanksAndQuotes(l, 0)
	markers := 0
	for {
		after := listMarkerEnd(l, pos)
		if after == pos {
			break
		}
		if after >= len(l) || !isBlank(l[after]) {
			return "", false
		}
		markers++
		pos = skipBlanksAndQuotes(l, after)
	}
	if markers == 0 || len(l) < pos+taskBoxLen || l[pos] != '[' || l[pos+2] != ']' || !unicode.IsSpace(l[pos+1]) {
		return "", false
	}
	return strings.Trim(string(l[pos+taskBoxLen:]), " \t"), true
}

// looseHeading is headingOf on the line without the blanks and quote marks in front of it, so that a
// heading inside a quote or an indented block is found too.
func looseHeading(l line) (level int, title string, ok bool) {
	return headingOf(l[skipBlanksAndQuotes(l, 0):])
}
