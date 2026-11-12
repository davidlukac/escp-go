package model

import (
	"strconv"
	"unicode/utf8"
)

// maxQuotedBytes is how much of a refused value an error message quotes: enough to recognize the
// value, short enough that a huge input does not become a huge error.
const maxQuotedBytes = 40

// quoteBounded is the Go-quoted form of s for an error message: all of s when it is at most
// maxQuotedBytes long, otherwise its first maxQuotedBytes bytes (shortened to a rune boundary, by at most UTFMax-1 bytes),
// quoted, followed by `... (N bytes)` with the full length.
func quoteBounded(s string) string {
	if len(s) <= maxQuotedBytes {
		return strconv.Quote(s)
	}
	cut := maxQuotedBytes
	for i := 1; i < utf8.UTFMax && !utf8.RuneStart(s[cut]); i++ { // a rune is at most UTFMax bytes: stop there on invalid UTF-8
		cut--
	}
	return strconv.Quote(s[:cut]) + "... (" + strconv.Itoa(len(s)) + " bytes)"
}
