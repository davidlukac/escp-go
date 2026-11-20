package verdict

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// plainReading is the oracle of FuzzJudge: the answer read as plainly as possible, with no code of the
// package under test. It returns the number of findings of the rule and of the others, and false when
// the plain reading rejects the answer (not valid JSON, not a list, an element that is not an object with a
// non-empty string rule name under the exact key).
func plainReading(answer []byte) (allowedCount, otherCount int, ok bool) {
	if !utf8.Valid(answer) || !json.Valid(answer) {
		return 0, 0, false
	}
	var list []map[string]any
	if json.Unmarshal(answer, &list) != nil || list == nil {
		return 0, 0, false
	}
	for _, element := range list {
		rule, isString := element["ident"].(string)
		if element == nil || !isString || rule == "" {
			return 0, 0, false
		}
		if rule == AllowedRule {
			allowedCount++
		} else {
			otherCount++
		}
	}
	return allowedCount, otherCount, true
}

// checkLines fails the test for a line that holds a character other than printable ASCII or that would
// start a workflow command of a job log.
func checkLines(t *testing.T, lines []string) {
	t.Helper()
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimLeftFunc(line, unicode.IsSpace), "::") {
			t.Fatalf("a workflow command in the output: %q", line)
		}
		if !printableLines(line) {
			t.Fatalf("an unprintable character in the output line %q", line)
		}
	}
}

// FuzzJudge: whatever the answer holds, the judgment ends without a panic, its output is printable ASCII
// with no line that starts a workflow command, and it passes only when a plain reading of the answer
// accepts it and finds exactly the expected findings of the rule and none of another: it never passes on an
// answer the plain reading rejects.
func FuzzJudge(f *testing.F) {
	sample := []byte(`[{"ident":"self-repository","desc":"d","locations":[{"symbolic":{"key":{"Local":{"verbatim_path":"a.yml"}}},"concrete":{"location":{"start_point":{"row":3}}}}]}]`)
	for _, seed := range [][]byte{
		nil, []byte("[]"), sample, []byte("{}"), []byte("null"), []byte("[null]"), []byte("[{}]"), []byte(`[{"ident":null}]`),
		[]byte(`[{"ident":""}]`), []byte(`[{"IDENT":"self-repository"}]`), []byte("[]\x00"), []byte("[] []"), []byte("\xef\xbb\xbf[]"),
		[]byte(`[{"ident":"self-repository"},{"ident":"template-injection","desc":"::error::x\n::add-mask::y"}]`),
		[]byte("[{\"ident\":\"self-repository\",\"desc\":\"\xff\"}]"), []byte(`[{"ident":"self-repository","ident":"x"}]`),
		[]byte(`[{"ident":"self-repository","locations":[{"concrete":{"location":{"start_point":{"row":9223372036854775807}}}}]}]`),
		[]byte(`[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]`), []byte(`[1e999]`), []byte(`["\ud800"]`),
	} {
		f.Add(1, seed)
		f.Add(0, seed)
	}
	f.Fuzz(func(t *testing.T, expected int, answer []byte) {
		if expected < 0 || expected > 1_000_000 {
			t.Skip("not an expected count ParseExpected gives")
		}
		got := Judge(expected, answer)
		checkLines(t, got.Lines)
		if len(got.Lines) == 0 {
			t.Fatal("no line")
		}
		if !got.Pass {
			return
		}
		allowedCount, otherCount, ok := plainReading(answer)
		if !ok || otherCount != 0 || allowedCount != expected {
			t.Fatalf("passed with %d expected on %q, but the plain reading finds ok=%v, %d allowed, %d other", expected, answer, ok, allowedCount, otherCount)
		}
	})
}

// plainNumber is what a plain non-negative integer of at most six digits looks like.
var plainNumber = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})$`)

// FuzzParseExpected: a number is accepted only when it is a plain non-negative integer of at most six
// digits, and then it is read back to the same text; anything else is an error whose text is printable ASCII.
func FuzzParseExpected(f *testing.F) {
	for _, seed := range []string{"", "0", "13", "010", "+1", "-1", " 1", "1 ", "1\n", "1e2", "0x1", "1_0", "٣", "999999", "1000000", "99999999999999999999"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		got, err := ParseExpected(text)
		if err != nil {
			if plainNumber.MatchString(text) {
				t.Fatalf("refused the plain number %q: %v", text, err)
			}
			if !printableLines(err.Error()) {
				t.Fatalf("an unprintable error for %q: %q", text, err)
			}
			return
		}
		if !plainNumber.MatchString(text) || strconv.Itoa(got) != text {
			t.Fatalf("accepted %q as %d", text, got)
		}
	})
}
