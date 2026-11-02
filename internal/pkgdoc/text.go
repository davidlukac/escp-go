package main

import (
	"html"
	"strings"
	"unicode"
)

// skippedTags hold nothing to read: their text is dropped, with everything nested in them.
var skippedTags = map[string]bool{"script": true, "style": true, "svg": true, "nav": true, "header": true, "footer": true}

// blockTags start and end a line of text.
var blockTags = map[string]bool{
	"p": true, "div": true, "li": true, "h1": true, "h2": true, "h3": true, "h4": true, "pre": true, "tr": true,
	"section": true, "details": true, "summary": true, "br": true, "dt": true, "dd": true,
}

// headingLevels are the tags that print as Markdown headings.
var headingLevels = map[string]int{"h1": 1, "h2": 2, "h3": 3}

// rawTextTags hold text that is not markup up to their end tag.
var rawTextTags = map[string]bool{"script": true, "style": true}

// textBuilder collects the text of a page while its tags are walked.
type textBuilder struct {
	out   strings.Builder
	depth int // how many skipped tags are open
}

func (b *textBuilder) start(tag string) {
	if skippedTags[tag] {
		b.depth++
	}
	if blockTags[tag] {
		b.out.WriteString("\n")
	}
	if n := headingLevels[tag]; n > 0 {
		b.out.WriteString(strings.Repeat("#", n) + " ")
	}
}

func (b *textBuilder) end(tag string) {
	if skippedTags[tag] && b.depth > 0 {
		b.depth--
	}
	if blockTags[tag] {
		b.out.WriteString("\n")
	}
}

func (b *textBuilder) text(s string) {
	if b.depth == 0 {
		b.out.WriteString(html.UnescapeString(s))
	}
}

// htmlToText turns an HTML page into plain text: block elements become lines, the headings h1 to h3
// get a Markdown prefix, scripts, styles, navigation and graphics are dropped, runs of blank lines
// shrink to one, and no line ends in a space.
func htmlToText(page string) string {
	var b textBuilder
	for rest := page; rest != ""; {
		lt := strings.IndexByte(rest, '<')
		if lt < 0 {
			b.text(rest)
			break
		}
		b.text(rest[:lt])
		rest = rest[lt:]
		rest = consumeMarkup(&b, rest)
	}
	return tidy(b.out.String())
}

// consumeMarkup handles the construct at the start of s, which begins with '<', and returns what
// follows it.
func consumeMarkup(b *textBuilder, s string) string {
	switch {
	case strings.HasPrefix(s, "<!--"):
		return afterFirst(s[len("<!--"):], "-->")
	case strings.HasPrefix(s, "<!"), strings.HasPrefix(s, "<?"):
		return afterFirst(s[2:], ">")
	case strings.HasPrefix(s, "</") && len(s) > 2 && isLetter(s[2]):
		name, rest := tagName(s[2:])
		b.end(name)
		return afterFirst(rest, ">")
	case len(s) > 1 && isLetter(s[1]):
		return consumeStartTag(b, s[1:])
	}
	b.text("<") // a '<' that opens no tag is text
	return s[1:]
}

// consumeStartTag handles a start tag whose name begins s (after the '<') and returns what follows it.
func consumeStartTag(b *textBuilder, s string) string {
	name, rest := tagName(s)
	rest, selfClosing := skipAttributes(rest)
	b.start(name)
	if selfClosing {
		b.end(name)
		return rest
	}
	if rawTextTags[name] {
		return skipRawText(rest, name)
	}
	return rest
}

// tagName reads a tag name (lower-cased) from the start of s and returns it and the rest.
func tagName(s string) (name, rest string) {
	i := 0
	for i < len(s) && !strings.ContainsRune(" \t\r\n\f/>", rune(s[i])) {
		i++
	}
	return strings.ToLower(s[:i]), s[i:]
}

// skipAttributes skips to the '>' that ends a tag, stepping over quoted values, and reports whether
// the tag closed itself with "/>".
func skipAttributes(s string) (rest string, selfClosing bool) {
	var quote byte
	for i := range len(s) {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return s[i+1:], i > 0 && s[i-1] == '/'
		}
	}
	return "", false
}

// skipRawText skips the text of a script or style element up to its end tag, which it leaves for the
// caller to read.
func skipRawText(s, name string) string {
	if i := strings.Index(strings.ToLower(s), "</"+name); i >= 0 {
		return s[i:]
	}
	return ""
}

// afterFirst returns what follows the first sep in s, or nothing when there is none.
func afterFirst(s, sep string) string {
	if i := strings.Index(s, sep); i >= 0 {
		return s[i+len(sep):]
	}
	return ""
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// tidy removes the trailing white space of every line and keeps at most one blank line in a row, none
// at the start.
func tidy(text string) string {
	var kept []string
	for _, line := range splitLines(text) {
		line = strings.TrimRightFunc(line, unicode.IsSpace)
		if strings.TrimSpace(line) != "" || (len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) != "") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n") + "\n"
}

// splitLines splits text at the line boundaries of Unicode text (as Python's str.splitlines does, so
// the output matches the earlier script): \n, \r, \r\n, \v, \f, the separators 0x1c to 0x1e, 0x85,
// and U+2028 and U+2029. The result has no empty last element for a text that ends in a boundary.
func splitLines(text string) []string {
	var lines []string
	start := 0
	runes := []rune(text)
	var cur strings.Builder
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if !isLineBoundary(r) {
			cur.WriteRune(r)
			continue
		}
		if r == '\r' && i+1 < len(runes) && runes[i+1] == '\n' {
			i++
		}
		lines = append(lines, cur.String())
		cur.Reset()
		start = i + 1
	}
	if start < len(runes) {
		lines = append(lines, cur.String())
	}
	return lines
}

func isLineBoundary(r rune) bool {
	switch r {
	case '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}
