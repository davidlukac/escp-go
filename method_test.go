package escp

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// methodConstants lists the Method constants declared in method.go, read from the source so that a
// constant added there is checked without being added here.
func methodConstants(t *testing.T) []Method {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "method.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []Method
	for _, d := range file.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				t.Fatalf("unexpected spec %T", spec)
			}
			for i, n := range vs.Names {
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || !strings.HasPrefix(n.Name, "Method") {
					continue
				}
				out = append(out, Method(strings.Trim(lit.Value, `"`)))
			}
		}
	}
	return out
}

// A constant and a row of the settings table cannot drift apart: every constant names a method of
// the table, and every method of the table has a constant.
func TestTheMethodConstantsAreTheMethodsOfTheSettingsTable(t *testing.T) {
	consts := methodConstants(t)
	if len(consts) < 30 {
		t.Fatalf("found only %d constants; the scan is broken", len(consts))
	}
	for _, m := range consts {
		if err := m.Validate(); err != nil {
			t.Errorf("%s: %v", m, err)
		}
	}
	have := map[Method]bool{}
	for _, m := range consts {
		have[m] = true
	}
	for _, s := range Settings() {
		for _, r := range s.Reads {
			if !have[r] {
				t.Errorf("%s: the reader %s has no Method constant", s.Page, r)
			}
		}
		for _, w := range s.Writes {
			if !have[w.Name] {
				t.Errorf("%s: the writer %s has no Method constant", s.Page, w.Name)
			}
		}
	}
}

func TestParseMethod(t *testing.T) {
	for _, tc := range []struct {
		name, in string
		want     Method
		wantErr  bool
	}{
		{"a write", "SetPort", MethodSetPort, false},
		{"a read", "Dot1Q", MethodDot1Q, false},
		{"a typo", "SetPorts", "", true},
		{"another case", "setport", "", true},
		{"empty", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseMethod(tc.in)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("got %v, want ErrInvalid", err)
				}
				return
			}
			if err != nil || got != tc.want || got.String() != tc.in {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}

func TestMethodTextEncoding(t *testing.T) {
	text, err := MethodSetLAG.MarshalText()
	if err != nil || string(text) != "SetLAG" {
		t.Fatalf("marshal gave %q, %v", text, err)
	}
	var back Method
	if err := back.UnmarshalText(text); err != nil || back != MethodSetLAG {
		t.Fatalf("unmarshal gave %q, %v", back, err)
	}
	if _, err := Method("SetNothing").MarshalText(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a name that is no method must not be written out: %v", err)
	}
	back = MethodSetLED
	if err := back.UnmarshalText([]byte("SetNothing")); !errors.Is(err, ErrInvalid) || back != MethodSetLED {
		t.Fatalf("a refused name must leave the value alone: %q, %v", back, err)
	}
}

// operation takes the class from the table; the helpers add the ports and the group without
// touching the rest.
func TestAnOperationIsBuiltFromAMethod(t *testing.T) {
	op := MethodSetLAG.Operation().On(MustPortSet(2, 3)).Replacing(Group{LAG: 4})
	want := Operation{Name: MethodSetLAG, Impact: ImpactManagement, Ports: MustPortSet(2, 3), Replaces: Group{LAG: 4}}
	if op != want {
		t.Fatalf("got %+v, want %+v", op, want)
	}
	if got := Method("SetNothing").Operation(); got.Impact != 0 {
		t.Fatalf("a method outside the table has class %v, want none", got.Impact)
	}
}

// The names of the writers are written once, in method.go: any other file of the package uses the
// constants, so a typo is a compile error and not a write with no class.
func TestNoMethodNameIsSpelledOutOutsideMethodGo(t *testing.T) {
	// the writers only: a reader is named like the type it returns (QoS, IGMP), which is no mistake
	names := map[string]bool{}
	for _, s := range Settings() {
		for _, w := range s.Writes {
			names[string(w.Name)] = true
		}
	}
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == "method.go" {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && names[strings.Trim(lit.Value, `"`)] {
				t.Errorf("%s: %s is a method name; use its Method constant", fset.Position(lit.Pos()), lit.Value)
			}
			return true
		})
	}
}
