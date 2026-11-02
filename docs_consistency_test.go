package escp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// parsedPackage parses the non-test Go files of a directory of this module.
func parsedPackage(t *testing.T, dir string) []*ast.File {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	fset := token.NewFileSet()
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	return files
}

func readText(t *testing.T, path string) string {
	t.Helper()
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(text)
}

// Every error a caller can test for is in the table of docs/errors.md: the exported Err variables of
// escp and safety, and the typed errors of the gates.
func TestEveryErrorIsInTheErrorsGuide(t *testing.T) {
	guide := readText(t, "docs/errors.md")
	var names []string
	for _, dir := range []string{".", "safety"} {
		for _, f := range parsedPackage(t, dir) {
			for _, decl := range f.Decls {
				d, ok := decl.(*ast.GenDecl)
				if !ok {
					continue
				}
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if n.IsExported() && strings.HasPrefix(n.Name, "Err") {
								names = append(names, n.Name)
							}
						}
					case *ast.TypeSpec:
						if s.Name.IsExported() && strings.HasSuffix(s.Name.Name, "Error") {
							names = append(names, s.Name.Name)
						}
					}
				}
			}
		}
	}
	if len(names) < 12 {
		t.Fatalf("found only %d error names: the scan is broken (%v)", len(names), names)
	}
	for _, n := range names {
		if !strings.Contains(guide, n) {
			t.Errorf("docs/errors.md does not mention %s", n)
		}
	}
}

// validatorList names, in alphabetical order, every exported type of the package that has a Validate
// method: the types a front end can check before a write. The README and the guide on writing safely
// carry the list between markers, generated from the code (`make settings-docs`).
func validatorList(t *testing.T) string {
	t.Helper()
	var types []string
	for _, f := range parsedPackage(t, ".") {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "Validate" || len(fn.Recv.List) != 1 {
				continue
			}
			recv := fn.Recv.List[0].Type
			if star, ok := recv.(*ast.StarExpr); ok {
				recv = star.X
			}
			if id, ok := recv.(*ast.Ident); ok && id.IsExported() {
				types = append(types, id.Name)
			}
		}
	}
	sort.Strings(types)
	if len(types) < 20 {
		t.Fatalf("found only %d types with Validate: the scan is broken (%v)", len(types), types)
	}
	return backticked(types)
}
