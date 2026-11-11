package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func names(fs []Finding) string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Name)
	}
	return strings.Join(out, ",")
}

func TestWhichIdentifiersStateAGuarantee(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string // the names reported, comma-separated, in source order
	}{
		{name: "a function that sends nothing", src: "// Dial sends nothing to the switch.\nfunc Dial() {}\n", want: "Dial"},
		{name: "never", src: "// Dial never panics.\nfunc Dial() {}\n", want: "Dial"},
		{name: "at most once", src: "// Dial runs at most once.\nfunc Dial() {}\n", want: "Dial"},
		{name: "safe for concurrent use", src: "// Client is safe for concurrent use.\ntype Client struct{}\n", want: "Client"},
		{name: "safe to use from several goroutines", src: "// Client is safe to use from several goroutines.\ntype Client struct{}\n", want: "Client"},
		{name: "without sending", src: "// F validates without sending anything.\nfunc F() {}\n", want: "F"},
		{name: "does not block", src: "// F does not block.\nfunc F() {}\n", want: "F"},
		{name: "a doc comment folded over several lines", src: "// F sends\n// nothing at\n// all.\nfunc F() {}\n", want: "F"},
		{name: "a promise in a later sentence", src: "// F reads the value. It is never\n// cached.\nfunc F() {}\n", want: "F"},
		{name: "a doc comment without a promise", src: "// F reads the value.\nfunc F() {}\n", want: ""},
		{name: "a fact that has never been true is not a promise", src: "// F is a switch that has never been configured and was never tried.\nfunc F() {}\n", want: ""},
		{name: "no doc comment", src: "func F() {}\n", want: ""},
		{name: "an unexported function", src: "// f never panics.\nfunc f() {}\n", want: ""},
		{name: "a method of an exported type", src: "type T struct{}\n\n// Close sends nothing.\nfunc (T) Close() {}\n", want: "T.Close"},
		{name: "a method on a pointer receiver", src: "type T struct{}\n\n// Close sends nothing.\nfunc (t *T) Close() {}\n", want: "T.Close"},
		{name: "a method of a generic type", src: "type T[K any] struct{}\n\n// Get never blocks.\nfunc (t *T[K]) Get() {}\n", want: "T.Get"},
		{name: "a method of two type parameters", src: "type T[K, V any] struct{}\n\n// Get never blocks.\nfunc (t *T[K, V]) Get() {}\n", want: "T.Get"},
		{name: "a method of an unexported type", src: "type t struct{}\n\n// Close sends nothing.\nfunc (t) Close() {}\n", want: ""},
		{name: "an unexported method of an exported type", src: "type T struct{}\n\n// close sends nothing.\nfunc (T) close() {}\n", want: ""},
		{name: "a type with its doc on the group", src: "// T sends nothing.\ntype (\n\tT struct{}\n)\n", want: "T"},
		{name: "the doc of a group of constants is not the doc of each", src: "// These never change.\nconst (\n\tA = 1\n\tB = 2\n)\n", want: ""},
		{name: "a constant with its own doc in a group", src: "const (\n\t// A never changes.\n\tA = 1\n\tB = 2\n)\n", want: "A"},
		{name: "a lone constant takes the doc of its declaration", src: "// A never changes.\nconst A = 1\n", want: "A"},
		{name: "a variable", src: "// ErrX is never returned for a retry.\nvar ErrX = 1\n", want: "ErrX"},
		{name: "an exported field", src: "type T struct {\n\t// Logger never receives a value.\n\tLogger int\n\t// n never changes.\n\tn int\n}\n", want: "T.Logger"},
		{name: "a method of an interface", src: "type R interface {\n\t// Info never waits.\n\tInfo()\n}\n", want: "R.Info"},
		{name: "a field of an unexported type", src: "type t struct {\n\t// F never changes.\n\tF int\n}\n", want: ""},
		{name: "an alias", src: "// A is never a copy.\ntype A = int\n", want: "A"},
		{name: "a function type", src: "// Gate never blocks.\ntype Gate func()\n", want: "Gate"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := writeTree(t, map[string]string{"p/p.go": "package p\n\n" + tc.src})
			res, err := Scan(root)
			if err != nil {
				t.Fatal(err)
			}
			if got := names(res.Unmatched); got != tc.want {
				t.Errorf("reported %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAMarkerInATestOfThePackageMatchesTheGuarantee(t *testing.T) {
	const src = "package p\n\n// Dial sends nothing.\nfunc Dial() {}\n\ntype T struct{}\n\n// Close never blocks.\nfunc (T) Close() {}\n"
	tests := []struct {
		name          string
		files         map[string]string
		wantMatched   string
		wantUnmatched string
		wantStale     string
	}{
		{name: "no marker", files: map[string]string{}, wantUnmatched: "Dial,T.Close"},
		{name: "a marker on a line of its own", files: map[string]string{"p/p_test.go": "package p\n\n// guarantee: Dial\nfunc TestDial() {}\n"},
			wantMatched: "Dial", wantUnmatched: "T.Close"},
		{name: "a marker inside a function", files: map[string]string{"p/p_test.go": "package p\n\nfunc TestDial() {\n\t// guarantee: Dial\n}\n"},
			wantMatched: "Dial", wantUnmatched: "T.Close"},
		{name: "a marker with a space after the slashes missing", files: map[string]string{"p/p_test.go": "package p\n\n//guarantee: Dial\nfunc TestDial() {}\n"},
			wantMatched: "Dial", wantUnmatched: "T.Close"},
		{name: "a method is named with its type", files: map[string]string{"p/p_test.go": "package p\n\n// guarantee: T.Close\nfunc TestClose() {}\n"},
			wantMatched: "T.Close", wantUnmatched: "Dial"},
		{name: "the bare name of a method does not match", files: map[string]string{"p/p_test.go": "package p\n\n// guarantee: Close\nfunc TestClose() {}\n"},
			wantUnmatched: "Dial,T.Close", wantStale: "Close"},
		{name: "several names, with commas and backticks", files: map[string]string{"p/p_test.go": "package p\n\n// guarantee: `Dial`, T.Close\nfunc TestBoth() {}\n"},
			wantMatched: "Dial,T.Close"},
		{name: "a marker in the external test package of the directory", files: map[string]string{"p/p_ext_test.go": "package p_test\n\n// guarantee: Dial\nfunc TestDial() {}\n"},
			wantMatched: "Dial", wantUnmatched: "T.Close"},
		{name: "a marker in the test of another package does not match", files: map[string]string{"q/q_test.go": "package q\n\n// guarantee: Dial\nfunc TestDial() {}\n"},
			wantUnmatched: "Dial,T.Close", wantStale: "Dial"},
		{name: "a marker that is not in a test file does not count", files: map[string]string{"p/other.go": "package p\n\n// guarantee: Dial\nfunc x() {}\n"},
			wantUnmatched: "Dial,T.Close"},
		{name: "a marker of an identifier without a documented promise is not stale", files: map[string]string{
			"p/more.go":   "package p\n\n// Plain is a function.\nfunc Plain() {}\n",
			"p/p_test.go": "package p\n\n// guarantee: Plain\nfunc TestPlain() {}\n"},
			wantUnmatched: "Dial,T.Close"},
		{name: "a marker of an identifier that does not exist is stale", files: map[string]string{"p/p_test.go": "package p\n\n// guarantee: Gone\nfunc TestGone() {}\n"},
			wantUnmatched: "Dial,T.Close", wantStale: "Gone"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"p/p.go": src}
			for k, v := range tc.files {
				files[k] = v
			}
			res, err := Scan(writeTree(t, files))
			if err != nil {
				t.Fatal(err)
			}
			var stale []string
			for _, m := range res.Stale {
				stale = append(stale, m.Name)
			}
			if got := names(res.Matched); got != tc.wantMatched {
				t.Errorf("matched %q, want %q", got, tc.wantMatched)
			}
			if got := names(res.Unmatched); got != tc.wantUnmatched {
				t.Errorf("unmatched %q, want %q", got, tc.wantUnmatched)
			}
			if got := strings.Join(stale, ","); got != tc.wantStale {
				t.Errorf("stale %q, want %q", got, tc.wantStale)
			}
			if res.HasFindings() != (tc.wantUnmatched != "" || tc.wantStale != "") {
				t.Errorf("HasFindings = %v", res.HasFindings())
			}
		})
	}
}

func TestDirectoriesThatAreNotTheLibraryAreNotScanned(t *testing.T) {
	const src = "package p\n\n// F never panics.\nfunc F() {}\n"
	files := map[string]string{
		"devtools/x/x.go": src,
		"testdata/x/x.go": src,
		"examples/x/x.go": src,
		"build/x/x.go":    src,
		".git/x/x.go":     src,
		"vendor/x/x.go":   src,
		"p/p.go":          src,
		"p/notes.txt":     "not go",
		"internal/w/w.go": src,
	}
	res, err := Scan(writeTree(t, files))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range res.Unmatched {
		got = append(got, f.File)
	}
	if want := "internal/w/w.go,p/p.go"; strings.Join(got, ",") != want {
		t.Errorf("scanned %q, want %q", strings.Join(got, ","), want)
	}
}

func TestAFileThatDoesNotParseIsAnError(t *testing.T) {
	if _, err := Scan(writeTree(t, map[string]string{"p/p.go": "package p\n\nfunc {\n"})); err == nil || !strings.Contains(err.Error(), "p.go") {
		t.Fatalf("err = %v", err)
	}
}

func TestTheReportListsEachFindingAndTheCount(t *testing.T) {
	root := writeTree(t, map[string]string{
		"p/p.go":      "package p\n\n// A never panics.\nfunc A() {}\n\n// B sends nothing.\nfunc B() {}\n\n// C reads.\nfunc C() {}\n",
		"p/p_test.go": "package p\n\n// guarantee: B, Gone\nfunc TestB() {}\n",
	})
	var out, errOut bytes.Buffer
	if status := run([]string{"-root", root}, &out, &errOut); status != 0 {
		t.Fatalf("report mode must not fail: status %d, stderr %q", status, errOut.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	want := []string{
		"p/p.go:4: A: A never panics. (no test carries `// guarantee: A`)",
		"p/p_test.go:3: `// guarantee: Gone` names no exported identifier of its package",
		"guarantee (report mode): 2 exported identifiers state a guarantee; 1 have a test marker, 1 do not; 1 stale marker(s)",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("report:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

func TestStrictModeFailsOnFindingsAndOnlyThen(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		args  []string
		want  int
		err   string
	}{
		{name: "findings in report mode", files: map[string]string{"p/p.go": "package p\n\n// A never panics.\nfunc A() {}\n"}, want: 0},
		{name: "findings in strict mode", files: map[string]string{"p/p.go": "package p\n\n// A never panics.\nfunc A() {}\n"}, args: []string{"-strict"}, want: 1},
		{name: "a stale marker in strict mode", files: map[string]string{"p/p_test.go": "package p\n\n// guarantee: Gone\n"}, args: []string{"-strict"}, want: 1},
		{name: "nothing to report in strict mode", files: map[string]string{
			"p/p.go":      "package p\n\n// A never panics.\nfunc A() {}\n",
			"p/p_test.go": "package p\n\n// guarantee: A\n"}, args: []string{"-strict"}, want: 0},
		{name: "a bad flag", files: map[string]string{}, args: []string{"-nope"}, want: 2, err: "flag provided but not defined"},
		{name: "a file that does not parse", files: map[string]string{"p/p.go": "package p\n\nfunc {\n"}, want: 2, err: "guarantee:"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			args := append([]string{"-root", writeTree(t, tc.files)}, tc.args...)
			if got := run(args, &out, &errOut); got != tc.want {
				t.Fatalf("status %d, want %d (stdout %q, stderr %q)", got, tc.want, out.String(), errOut.String())
			}
			if !strings.Contains(errOut.String(), tc.err) {
				t.Errorf("stderr %q does not contain %q", errOut.String(), tc.err)
			}
		})
	}
}

func TestALongSentenceIsCut(t *testing.T) {
	doc := "// F never " + strings.Repeat("word ", 60) + "ends.\nfunc F() {}\n"
	res, err := Scan(writeTree(t, map[string]string{"p/p.go": "package p\n\n" + doc}))
	if err != nil {
		t.Fatal(err)
	}
	got := res.Unmatched[0].Sentence
	if len(got) > maxSentence+len("...") || !strings.HasSuffix(got, "...") {
		t.Errorf("sentence = %q (%d bytes)", got, len(got))
	}
}
