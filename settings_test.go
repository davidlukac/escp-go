package escp

import (
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
)

var updateDocs = flag.Bool("update-docs", false, "rewrite the tables generated from the settings table (README.md, docs/writing-safely.md)")

// registeredImpact is the class the settings table gives a write method, read through the public
// Settings so that it does not share code with the writers.
func registeredImpact(method string) Impact {
	for _, s := range Settings() {
		for _, w := range s.Writes {
			if w.Name == method {
				return w.Impact
			}
		}
	}
	return 0
}

// The table is the one list of what the client reads and writes: a method of the client that is not
// in it (or a row for a method that does not exist) is a mistake the tests name.
func TestTheSettingsTableListsEveryReaderAndWriter(t *testing.T) {
	typ := reflect.TypeOf(&Client{})
	var rows, written []string
	for _, s := range Settings() {
		for _, r := range s.Reads {
			rows = append(rows, r)
			if _, ok := typ.MethodByName(r); !ok {
				t.Errorf("%s: the table lists the reader %s, which Client does not have", s.Page, r)
			}
		}
		for _, w := range s.Writes {
			written = append(written, w.Name)
			if _, ok := typ.MethodByName(w.Name); !ok {
				t.Errorf("%s: the table lists the writer %s, which Client does not have", s.Page, w.Name)
			}
			if w.Impact.Validate() != nil {
				t.Errorf("%s: %s has no valid class", s.Page, w.Name)
			}
		}
		if len(s.Writes) == 0 && s.WriteNote == "" {
			t.Errorf("%s: a row without writers says why (WriteNote)", s.Page)
		}
	}
	for _, name := range writeMethodNames() {
		if !slices.Contains(written, name) {
			t.Errorf("%s changes the switch but is not in the settings table (settings.go): add it with its class", name)
		}
	}
	// the readers of the Reader interface and the others the guides name are all in the table
	for i := range reflect.TypeOf((*Reader)(nil)).Elem().NumMethod() {
		name := reflect.TypeOf((*Reader)(nil)).Elem().Method(i).Name
		if !slices.Contains(rows, name) {
			t.Errorf("%s is part of Reader but is not in the settings table", name)
		}
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

func TestAMethodThatIsNotInTheSettingsTableHasNoClass(t *testing.T) {
	if got := impactOf("SetNothing"); got != 0 {
		t.Fatalf("an unknown method has class %v, want none (the gates refuse the zero class)", got)
	}
	if got := impactOf("SetPort"); got != ImpactManagement {
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

// settingsTable is the README table: one row per page.
func settingsTable() string {
	var b strings.Builder
	b.WriteString("| Setting (web UI page) | Read | Write | Types | Impact of the write |\n|---|---|---|---|---|\n")
	for _, s := range Settings() {
		var writes []string
		for _, w := range s.Writes {
			writes = append(writes, "`"+w.Name+"`")
		}
		write := strings.Join(writes, ", ")
		if len(writes) == 0 {
			write = "(" + s.WriteNote + ")"
		}
		impact := impactCell(s)
		if impact == "" {
			impact = "(none)"
		}
		b.WriteString("| " + s.Page + " | " + backticked(s.Reads) + " | " + write + " | " + backticked(s.Types) + " | " + impact + " |\n")
	}
	return b.String()
}

func backticked(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "`" + n + "`"
	}
	return strings.Join(quoted, ", ")
}

// impactCell is the class of a row's writes, or, when they differ, each class with its methods.
func impactCell(s Setting) string {
	if len(s.Writes) == 0 {
		return ""
	}
	byClass := map[Impact][]string{}
	for _, w := range s.Writes {
		byClass[w.Impact] = append(byClass[w.Impact], w.Name)
	}
	if len(byClass) == 1 {
		return s.Writes[0].Impact.String()
	}
	var parts []string
	for _, class := range []Impact{ImpactCosmetic, ImpactTraffic, ImpactManagement} {
		if names := byClass[class]; len(names) > 0 {
			parts = append(parts, class.String()+" ("+strings.Join(names, ", ")+")")
		}
	}
	return strings.Join(parts, ", ")
}

// impactClasses lists the methods of each class, for the guide on writing safely.
func impactClasses() string {
	var b strings.Builder
	b.WriteString("| Class | Methods |\n|---|---|\n")
	for _, class := range []Impact{ImpactCosmetic, ImpactTraffic, ImpactManagement} {
		var names []string
		for _, s := range Settings() {
			for _, w := range s.Writes {
				if w.Impact == class {
					names = append(names, w.Name)
				}
			}
		}
		b.WriteString("| `Impact" + strings.ToUpper(class.String()[:1]) + class.String()[1:] + "` | " + backticked(names) + " |\n")
	}
	return b.String()
}

// The tables that come from the settings table are not written by hand: they are generated between
// markers (`make settings-docs` rewrites them), and this test fails when one is stale.
func TestTheGeneratedTablesAreCurrent(t *testing.T) {
	for _, g := range []struct {
		file, name, content string
	}{
		{"README.md", "settings-table", settingsTable()},
		{"docs/writing-safely.md", "impact-classes", impactClasses()},
		{"README.md", "validators", validatorList(t)},
		{"docs/writing-safely.md", "validators", validatorList(t)},
	} {
		begin := "<!-- " + g.name + ":begin (generated from escp.Settings: make settings-docs) -->"
		end := "<!-- " + g.name + ":end -->"
		text, err := os.ReadFile(g.file)
		if err != nil {
			t.Fatal(err)
		}
		doc := string(text)
		i, j := strings.Index(doc, begin), strings.Index(doc, end)
		if i < 0 || j < i {
			t.Fatalf("%s has no %s block between\n%s\nand\n%s", g.file, g.name, begin, end)
		}
		want := doc[:i+len(begin)] + "\n\n" + g.content + "\n" + doc[j:]
		if !strings.Contains(g.content, "\n") { // a list inside a sentence
			want = doc[:i+len(begin)] + g.content + doc[j:]
		}
		if want == doc {
			continue
		}
		if *updateDocs {
			if err := os.WriteFile(g.file, []byte(want), 0o600); err != nil {
				t.Fatal(err)
			}
			continue
		}
		t.Errorf("%s is out of date with escp.Settings: run `make settings-docs` and commit the result", g.file)
	}
}
