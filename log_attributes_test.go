package escp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// logLevels are the slog methods that take a context; the library logs through nothing else.
var logLevels = map[string]bool{"DebugContext": true, "InfoContext": true, "WarnContext": true, "ErrorContext": true}

// loggedAttributes returns every attribute name that the production code of this package passes to
// the logger: the string literals at the even positions after the context and the message, and the
// names given to Logger.With.
func loggedAttributes(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			first := 0 // the index of the first attribute name among the arguments
			switch {
			case logLevels[sel.Sel.Name] && len(call.Args) >= 2:
				first = 2 // after the context and the message
			case sel.Sel.Name == "With" && len(call.Args) >= 2:
				// a Logger.With(key, value, ...) whose key is a literal; a With on anything else has none
			default:
				return true
			}
			for i := first; i < len(call.Args); i += 2 {
				if lit, ok := call.Args[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					found[strings.Trim(lit.Value, `"`)] = name
				}
			}
			return true
		})
	}
	return found
}

// The names of the log attributes are a contract (docs/logging.md): a name the code logs must be
// in the table there, so that a front end that filters on it can rely on it.
func TestEveryLoggedAttributeIsInTheLoggingGuide(t *testing.T) {
	guide, err := os.ReadFile("docs/logging.md")
	if err != nil {
		t.Fatal(err)
	}
	attributes := loggedAttributes(t)
	if len(attributes) < 20 {
		t.Fatalf("found only %d attributes in the code: the scan is broken (%v)", len(attributes), attributes)
	}
	for name, file := range attributes {
		if !strings.Contains(string(guide), "`"+name+"`") {
			t.Errorf("%s logs the attribute %q, which docs/logging.md does not list", file, name)
		}
	}
}
