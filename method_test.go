package escp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davidlukac/escp-go/model"
)

// The names of the writers are written once, in method.go: any other file of the package uses the
// constants, so a typo is a compile error and not a write with no class.
func TestNoMethodNameIsSpelledOutOutsideMethodGo(t *testing.T) {
	// the writers only: a reader is named like the type it returns (QoS, IGMP), which is no mistake
	names := map[string]bool{}
	for _, s := range model.Settings() {
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
