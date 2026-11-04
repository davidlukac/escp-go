package model

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The model is the lowest layer of the library: it must not import the client, the policy or the
// fake, and of the module's internals only the wire lengths and the error class.
func TestTheModelImportsNothingAboveIt(t *testing.T) {
	allowed := map[string]bool{
		"github.com/davidlukac/escp-go/internal/wire":    true,
		"github.com/davidlukac/escp-go/internal/invalid": true,
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(path, "github.com/davidlukac/escp-go") && !allowed[path] {
				t.Errorf("%s imports %s: the model must not depend on it", name, path)
			}
		}
	}
}
