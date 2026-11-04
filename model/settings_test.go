package model

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

func TestAMethodThatIsNotInTheSettingsTableHasNoClass(t *testing.T) {
	if got := impactOf("SetNothing"); got != 0 {
		t.Fatalf("an unknown method has class %v, want none (the gates refuse the zero class)", got)
	}
	if got := impactOf(MethodSetPort); got != ImpactManagement {
		t.Fatalf("SetPort has class %v", got)
	}
}

func TestSettingsIsACopy(t *testing.T) {
	got := Settings()
	got[0].Reads[0] = "tampered"
	got[0].Writes[0].Name = "tampered"
	got[0].Types[0] = "tampered"
	again := Settings()
	if again[0].Reads[0] == "tampered" || again[0].Writes[0].Name == "tampered" || again[0].Types[0] == "tampered" {
		t.Fatal("changing the result changed the table")
	}
}

func TestEveryTypeOfTheSettingsTableExists(t *testing.T) {
	exported := map[string]bool{"bool": true}
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".go") || strings.HasSuffix(f.Name(), "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, f.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(n ast.Node) bool {
			if spec, ok := n.(*ast.TypeSpec); ok && spec.Name.IsExported() {
				exported[spec.Name.Name] = true
			}
			return true
		})
	}
	for _, s := range Settings() {
		for _, typ := range s.Types {
			if !exported[typ] {
				t.Errorf("%s: the table lists the type %s, which the package does not export", s.Page, typ)
			}
		}
	}
}
