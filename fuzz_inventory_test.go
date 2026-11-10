package escp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// DC-L06: a parser of bytes that came from the network or a file has a fuzz target. This file is the
// check that says so for every parser, so that a new one cannot be added without one.
//
// What is a parser (the rule, applied to the non-test Go files of parserDirs): a function or method
//
//   - whose name begins with parse, decode or unmarshal (any case), or is listed in otherParsers, and
//   - that takes at least one parameter of a type a peer or a file fills: []byte, string, [][]byte
//     or a TLV list (see isRawParam; otherParsers are exempt from this half, because they are named).
//
// What is a target: a function named Fuzz* in a test file of the same directory, together with the
// test helpers it calls (by name, through any depth), that mentions the parser by name (a call, or a
// reference to it as a value); a method also needs the name of its receiver type to be mentioned, since
// the same method name (UnmarshalText) belongs to many types. A parser with no target is either given
// one, or listed in noTargetNeeded with the reason, which is read by a person at review.

// parserDirs are the directories (relative to the module root) whose parsers the rule covers: the
// client, the value types, the gates and the wire format. The tools (internal/release, coverpolicy,
// the documentation and fuzz commands) read files of this repository, not bytes of the network.
var parserDirs = []string{".", "model", "safety", "internal/wire"}

// otherParsers are parsers that are not named parse*, decode* or unmarshal*: each reads a reply, a
// packet or a file into values. A name here that no longer exists fails the test, so the list cannot go stale.
var otherParsers = []string{
	"infoFromTLVs",     // the fields of a discovery reply
	"infoFromReply",    // the same, from a reply
	"ipConfigFromInfo", // the address triple a write would be built from
	"assembleConfig",   // the packets of a configuration download into the file
	"speedFromCode",    // the speed code of a port entry
	"MACFromBytes",     // the six bytes of a TLV into a MAC
	"CString",          // the NUL-terminated text of a reply field
	"LooksLikeHeader",  // the check that decides whether a buffer is a packet
}

// noTargetNeeded lists the parsers without a target of their own, each with why, by "dir:Name" (a
// method "dir:Type.Name"). An entry that has a target, or names no parser, fails the test.
var noTargetNeeded = map[string]string{
	".:infoFromReply": "one line that hands the TLVs of a reply to infoFromTLVs, which FuzzInfoFromTLVs and FuzzReplyPayload drive",
	".:speedFromCode": "total over a byte and called only by parsePorts and parsePortStats, which every parser target drives (FuzzSettingParsers, FuzzParserBounds)",
}

// isRawParam reports whether a parameter type is one a peer or a file fills: bytes, text, packets of
// bytes or TLVs.
func isRawParam(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name == "string"
	case *ast.ArrayType:
		switch elt := x.Elt.(type) {
		case *ast.Ident:
			return elt.Name == "byte" || elt.Name == "TLV"
		case *ast.SelectorExpr:
			return elt.Sel.Name == "TLV"
		case *ast.ArrayType:
			return isRawParam(elt)
		}
	}
	return false
}

// parserName is the key of a parser: the name, or Type.Name for a method.
func parserName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name + "." + fn.Name.Name
	}
	return fn.Name.Name
}

// isParser applies the rule above to one declaration.
func isParser(fn *ast.FuncDecl, extra []string) bool {
	if fn.Body == nil {
		return false
	}
	lower := strings.ToLower(fn.Name.Name)
	if slices.Contains(extra, fn.Name.Name) {
		return true
	}
	if !strings.HasPrefix(lower, "parse") && !strings.HasPrefix(lower, "decode") && !strings.HasPrefix(lower, "unmarshal") {
		return false
	}
	for _, p := range fn.Type.Params.List {
		if isRawParam(p.Type) {
			return true
		}
	}
	return false
}

// parsedDir is the Go files of one directory, parsed.
type parsedDir struct {
	prod, tests []*ast.File
}

// parseSources parses named sources into a parsedDir; a name ending in _test.go is a test file.
func parseSources(sources map[string]string) (parsedDir, error) {
	var d parsedDir
	fset := token.NewFileSet()
	names := make([]string, 0, len(sources))
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f, err := parser.ParseFile(fset, name, sources[name], 0)
		if err != nil {
			return d, err
		}
		if strings.HasSuffix(name, "_test.go") {
			d.tests = append(d.tests, f)
		} else {
			d.prod = append(d.prod, f)
		}
	}
	return d, nil
}

// readDir parses the Go files of a directory on disk.
func readDir(dir string) (parsedDir, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return parsedDir{}, err
	}
	sources := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return parsedDir{}, err
		}
		sources[e.Name()] = string(b)
	}
	return parseSources(sources)
}

// mentionedByTargets returns every name a fuzz target of the directory mentions, directly or through
// the test helpers it calls.
func (d parsedDir) mentionedByTargets() map[string]bool {
	helpers := map[string]ast.Node{} // test functions and package-level variables, by name
	var targets []*ast.FuncDecl
	for _, f := range d.tests {
		for _, decl := range f.Decls {
			switch x := decl.(type) {
			case *ast.FuncDecl:
				if x.Body == nil || x.Recv != nil {
					continue
				}
				if strings.HasPrefix(x.Name.Name, "Fuzz") {
					targets = append(targets, x)
				} else {
					helpers[x.Name.Name] = x.Body
				}
			case *ast.GenDecl:
				for _, spec := range x.Specs {
					if v, ok := spec.(*ast.ValueSpec); ok {
						for _, name := range v.Names {
							helpers[name.Name] = v
						}
					}
				}
			}
		}
	}
	seen := map[string]bool{}
	var visit func(n ast.Node)
	visit = func(root ast.Node) {
		ast.Inspect(root, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || seen[id.Name] {
				return true
			}
			seen[id.Name] = true
			if h, ok := helpers[id.Name]; ok {
				visit(h)
			}
			return true
		})
	}
	for _, fn := range targets {
		visit(fn.Body)
	}
	return seen
}

// withoutTarget lists the parsers of the directory (by parserName) that no fuzz target mentions.
func (d parsedDir) withoutTarget(extra []string) []string {
	mentioned := d.mentionedByTargets()
	var missing []string
	for _, f := range d.prod {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !isParser(fn, extra) {
				continue
			}
			name := parserName(fn)
			covered := mentioned[fn.Name.Name]
			if recv, _, isMethod := strings.Cut(name, "."); isMethod {
				covered = covered && mentioned[recv]
			}
			if !covered {
				missing = append(missing, name)
			}
		}
	}
	sort.Strings(missing)
	return slices.Compact(missing)
}

// TestEveryParserHasAFuzzTarget (DC-L06, escp-go#193): every parser of the rule above has a fuzz target
// or an entry in noTargetNeeded. Adding a parse* function without one fails this test.
func TestEveryParserHasAFuzzTarget(t *testing.T) {
	used := map[string]bool{}
	for _, dir := range parserDirs {
		t.Run(dir, func(t *testing.T) {
			d, err := readDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range d.withoutTarget(otherParsers) {
				key := dir + ":" + name
				if _, ok := noTargetNeeded[key]; ok {
					used[key] = true
					continue
				}
				t.Errorf("%s has no fuzz target: add a Fuzz function that calls it (DC-L06), or list it in noTargetNeeded with the reason", key)
			}
		})
	}
	for key, reason := range noTargetNeeded {
		t.Run("exception "+key, func(t *testing.T) {
			if !used[key] {
				t.Errorf("noTargetNeeded lists %s (%s), which has a fuzz target now or is no parser: remove the entry", key, reason)
			}
			if strings.TrimSpace(reason) == "" {
				t.Errorf("noTargetNeeded lists %s without a reason", key)
			}
		})
	}
}

// TestOtherParsersExist keeps otherParsers honest: every name in it is a function of the module
// that the rule would not find by its name, so that a rename or a removal is noticed.
func TestOtherParsersExist(t *testing.T) {
	found := map[string]bool{}
	for _, dir := range parserDirs { // subtests: this only collects the functions; the cases are the names below
		d, err := readDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range d.prod {
			for _, decl := range f.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && isParser(fn, otherParsers) && !isParser(fn, nil) {
					found[fn.Name.Name] = true
				}
			}
		}
	}
	for _, name := range otherParsers {
		t.Run(name, func(t *testing.T) {
			if !found[name] {
				t.Errorf("otherParsers lists %s, which is no function of %v or is found by its name already", name, parserDirs)
			}
		})
	}
}

// TestParserRuleFindsWhatItShould runs the rule on small sources, so that a change of the rule that
// stops it from seeing a parser, or lets a missing target pass, fails here and not silently.
func TestParserRuleFindsWhatItShould(t *testing.T) {
	const prod = `package p
func parseThing(b []byte) (int, error) { return 0, nil }
func decodeText(s string) int { return 0 }
func ParseAll(parts [][]byte) int { return 0 }
func parseTLVs(tlvs []wire.TLV) int { return 0 }
func (m *Mode) UnmarshalText(text []byte) error { return nil }
func (m *Other) UnmarshalText(text []byte) error { return nil }
func parseCount(n int) int { return n }
func helperFrom(b []byte) int { return 0 }
func parseNoBody(b []byte) int
`
	tests := []struct {
		name  string
		tests string
		extra []string
		want  []string
	}{
		{"no target at all", "package p", nil,
			[]string{"Mode.UnmarshalText", "Other.UnmarshalText", "ParseAll", "decodeText", "parseTLVs", "parseThing"}},
		{"a call is a target", `package p
import "testing"
func FuzzAll(f *testing.F) { f.Fuzz(func(t *testing.T, b []byte) { parseThing(b); decodeText(""); ParseAll(nil); parseTLVs(nil) }) }`, nil,
			[]string{"Mode.UnmarshalText", "Other.UnmarshalText"}},
		{"a method needs its type", `package p
import "testing"
func FuzzMode(f *testing.F) { f.Fuzz(func(t *testing.T, b []byte) { var m Mode; _ = m.UnmarshalText(b) }) }`, nil,
			[]string{"Other.UnmarshalText", "ParseAll", "decodeText", "parseTLVs", "parseThing"}},
		{"a reference as a value is a target", `package p
import "testing"
var table = []func(string) int{decodeText}
func FuzzTable(f *testing.F) { f.Fuzz(func(t *testing.T, s string) { _ = table }) }`, nil,
			[]string{"Mode.UnmarshalText", "Other.UnmarshalText", "ParseAll", "parseTLVs", "parseThing"}},
		{"a helper that a target calls", `package p
import "testing"
func helper(b []byte) { parseThing(b) }
func FuzzHelper(f *testing.F) { f.Fuzz(func(t *testing.T, b []byte) { helper(b) }) }
func notATarget(b []byte) { decodeText("") }`, nil,
			[]string{"Mode.UnmarshalText", "Other.UnmarshalText", "ParseAll", "decodeText", "parseTLVs"}},
		{"a test that is no target does not count", `package p
import "testing"
func TestNotFuzz(t *testing.T) { parseThing(nil); decodeText("") }`, nil,
			[]string{"Mode.UnmarshalText", "Other.UnmarshalText", "ParseAll", "decodeText", "parseTLVs", "parseThing"}},
		{"a named parser is listed", `package p`, []string{"helperFrom"},
			[]string{"Mode.UnmarshalText", "Other.UnmarshalText", "ParseAll", "decodeText", "helperFrom", "parseTLVs", "parseThing"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := parseSources(map[string]string{"p.go": prod, "p_test.go": tt.tests})
			if err != nil {
				t.Fatal(err)
			}
			if got := d.withoutTarget(tt.extra); !slices.Equal(got, tt.want) {
				t.Fatalf("without a target: %v, want %v", got, tt.want)
			}
		})
	}
}
