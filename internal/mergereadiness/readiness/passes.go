package readiness

import "strings"

const (
	// sectionTitle is the normalized title of the section's heading.
	sectionTitle = "merge readiness"
	// sectionLevel is the level of the section's heading; a heading of this level or a shallower one ends it.
	sectionLevel = 2
)

// splitLines is the description as lines, whatever its line endings.
func splitLines(body string) []line {
	text := strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n")
	parts := strings.Split(text, "\n")
	lines := make([]line, len(parts))
	for i, part := range parts {
		lines[i] = line(part)
	}
	return lines
}

// structure is what the structured reading found.
type structure struct {
	sections int      // the merge readiness sections
	unticked []string // the text of each unticked box in them
	ticked   int      // the number of ticked boxes in them
}

// commentState follows the HTML comments across lines.
type commentState struct {
	// closesLater[i] tells whether a line after line i holds the end of a comment.
	closesLater []bool
	open        bool
}

// newCommentState prepares the look-ahead for the lines.
func newCommentState(lines []line) *commentState {
	state := &commentState{closesLater: make([]bool, len(lines))}
	seen := false
	for i := len(lines) - 1; i >= 0; i-- {
		state.closesLater[i] = seen
		seen = seen || indexFrom(lines[i], 0, commentClose) >= 0
	}
	return state
}

// visibleLines calls visit with the text of each line that the structured reading looks at: a line in a
// fenced code block, or in a comment that is closed later, is not looked at.
func visibleLines(lines []line, visit func(line)) {
	comments := newCommentState(lines)
	var fence line
	for i, l := range lines {
		if fence != nil {
			if closesFence(l, fence) {
				fence = nil
			}
			continue
		}
		pos := 0
		if comments.open {
			end := indexFrom(l, 0, commentClose)
			if end < 0 {
				continue
			}
			pos = end + len(commentClose)
			comments.open = false
		} else if fence = fenceMarker(l); fence != nil {
			continue
		}
		var open bool
		pos, open = skipLeadingComments(l, pos, comments.closesLater[i])
		comments.open = open
		if !open {
			visit(l[pos:])
		}
	}
}

// skipLeadingComments skips the comments that start at pos, after up to three spaces. It returns the
// position after them and whether the last one stays open on the following lines. A comment that is
// never closed is text: it hides nothing.
func skipLeadingComments(l line, pos int, closesLater bool) (next int, open bool) {
	for {
		indent := countIndent(l[pos:], maxIndent+1)
		start := pos + indent
		if indent > maxIndent || !hasAt(l, start, commentOpen) {
			return pos, false
		}
		end := indexFrom(l, start+len(commentOpen), commentClose)
		if end < 0 {
			if closesLater {
				return len(l), true
			}
			return pos, false
		}
		pos = end + len(commentClose)
	}
}

// structuredPass reads the headings, fences and comments, and the boxes of the sections.
func structuredPass(lines []line) structure {
	var found structure
	inside := false
	visibleLines(lines, func(l line) {
		if level, title, ok := headingOf(l); ok && level <= sectionLevel {
			inside = level == sectionLevel && title == sectionTitle
			if inside {
				found.sections++
			}
			return
		}
		if !inside {
			return
		}
		switch unticked, text, ok := strictBox(l); {
		case !ok:
		case unticked:
			found.unticked = append(found.unticked, text)
		default:
			found.ticked++
		}
	})
	return found
}

// naivePass reads no structure: it reports whether a heading titled "merge readiness" (at any level, in
// a quote too) exists, and the text of every line after one that has the shape of an unticked box, up
// to the next heading of level 1 or 2.
func naivePass(lines []line) (found bool, boxes []string) {
	inside := false
	for _, l := range lines {
		if level, title, ok := looseHeading(l); ok {
			if title == sectionTitle {
				inside, found = true, true
				continue
			}
			if level <= sectionLevel {
				inside = false
				continue
			}
		}
		if !inside {
			continue
		}
		if text, ok := naiveBox(l); ok {
			boxes = append(boxes, text)
		}
	}
	return found, boxes
}
