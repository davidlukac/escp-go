package escp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// notInTheSettingsTable is every exported method of Client that is neither a reader nor a writer of a page
// of the settings table, with why. A method that is not here and not in the table is a decision nobody
// took: TestEveryExportedClientMethodIsInTheSettingsTableOrNamed names it.
var notInTheSettingsTable = map[string]string{
	"Close":     "ends the client; it talks to no page",
	"SwitchMAC": "the identity of the switch, an accessor and no page of the web UI",
}

// The settings table is the one list of what the client reads and writes, and a writer is found by what
// it does, not by the prefix of its name: every exported method of Client is a reader or a writer of the
// table, or is named above with its reason, so a method called `Reboot` or `Restore` is not missed by the
// tests that look for `Set`, `Delete` and `Enable`.
func TestEveryExportedClientMethodIsInTheSettingsTableOrNamed(t *testing.T) {
	inTable := map[string]string{}
	for _, s := range Settings() {
		for _, r := range s.Reads {
			inTable[string(r)] = "reads " + s.Page
		}
		for _, w := range s.Writes {
			inTable[string(w.Name)] = "writes " + s.Page
		}
	}
	typ := reflect.TypeFor[*Client]()
	if typ.NumMethod() < 30 { // a floor against a broken scan, not a count of the methods
		t.Fatalf("found only %d exported methods of Client by reflection; the scan is broken", typ.NumMethod())
	}
	for method := range typ.Methods() {
		name := method.Name
		t.Run(name, func(t *testing.T) {
			reason, named := notInTheSettingsTable[name]
			switch {
			case inTable[name] == "" && !named:
				t.Errorf("%s is an exported method of Client that the settings table does not list (settings.go): decide which it is; a reader or a writer gets a row with its page and, for a writer, its impact class, anything else is named in notInTheSettingsTable with its reason", name)
			case inTable[name] != "" && named:
				t.Errorf("%s is in the settings table (%s) and also named as no page (%q): remove one", name, inTable[name], reason)
			}
		})
	}
	for name := range notInTheSettingsTable {
		t.Run("named "+name, func(t *testing.T) {
			if _, ok := typ.MethodByName(name); !ok {
				t.Errorf("notInTheSettingsTable names %s, which Client does not have: remove it", name)
			}
		})
	}
}

// sendPathRoots are the functions every write of the switch ends in: setTLVsSent is the one place that
// sends a SET behind the write gate, and setTLVs and writeSystem are its two callers' entry points.
var sendPathRoots = []string{"setTLVs", "setTLVsSent", "writeSystem"}

// clientCallGraph reads the production sources of the package and returns, for each method of Client, the
// names of the methods of Client its body calls (by name: `x.Name(...)` with a Name that Client has).
func clientCallGraph(t *testing.T) map[string][]string {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("cannot list the package sources: %v", err)
	}
	fset := token.NewFileSet()
	bodies := map[string]*ast.BlockStmt{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil && receiverIsClient(fn) {
				bodies[fn.Name.Name] = fn.Body
			}
		}
	}
	graph := map[string][]string{}
	for name, body := range bodies {
		ast.Inspect(body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && bodies[sel.Sel.Name] != nil && !slices.Contains(graph[name], sel.Sel.Name) {
					graph[name] = append(graph[name], sel.Sel.Name)
				}
			}
			return true
		})
	}
	return graph
}

// receiverIsClient reports whether fn is a method of Client (or *Client).
func receiverIsClient(fn *ast.FuncDecl) bool {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return false
	}
	t := fn.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	id, ok := t.(*ast.Ident)
	return ok && id.Name == "Client"
}

// reaching returns the methods of the graph that reach one of the roots through calls (the roots included).
func reaching(graph map[string][]string, roots []string) map[string]bool {
	reach := map[string]bool{}
	for _, r := range roots {
		reach[r] = true
	}
	for changed := true; changed; {
		changed = false
		for name, calls := range graph {
			if reach[name] {
				continue
			}
			for _, callee := range calls {
				if reach[callee] {
					reach[name], changed = true, true
					break
				}
			}
		}
	}
	return reach
}

// A writer is also found by its body: every exported method of Client that reaches the single place a SET is
// sent behind the write gate is in the table as a writer with its class, whatever it is called, and every
// writer of the table does reach it (a row for a method that no longer writes is stale).
func TestEveryExportedClientMethodThatReachesTheSendPathIsAWriterOfTheTable(t *testing.T) {
	graph := clientCallGraph(t)
	reach := reaching(graph, sendPathRoots)
	written := map[string]bool{}
	for _, s := range Settings() {
		for _, w := range s.Writes {
			written[string(w.Name)] = true
		}
	}
	if len(written) < 17 { // a floor against a broken table scan
		t.Fatalf("found only %d writers in the settings table; the scan is broken", len(written))
	}
	for method := range reflect.TypeFor[*Client]().Methods() {
		name := method.Name
		t.Run(name, func(t *testing.T) {
			switch {
			case reach[name] && !written[name]:
				t.Errorf("%s reaches the send path (it changes the switch) but the settings table does not list it as a writer: add it with its impact class", name)
			case !reach[name] && written[name]:
				t.Errorf("%s is a writer of the settings table but does not reach the send path: the row is stale, or the scan does not see how it writes", name)
			}
		})
	}
}

// noRefusableArgument is each writer of the table that takes an argument and has no refused-argument row,
// with why: its argument has no invalid value.
var noRefusableArgument = map[Method]string{
	MethodSetLED:            "a bool: nothing to refuse",
	MethodSetLoopPrevention: "a bool: nothing to refuse",
	MethodSetIGMP:           "IGMPConfig is two booleans: nothing to refuse",
}

// A new writer cannot skip the refused-argument case: every writer of the settings table that takes an
// argument besides the context has a row in the table of TestEveryRefusedArgumentIsErrInvalid (a row is
// named after the writer it calls, `SetPVIDs empty`), or is named in noRefusableArgument with its reason.
func TestEveryWriterWithAnArgumentHasARefusedArgumentRow(t *testing.T) {
	rowsOf := map[string]int{}
	for _, row := range refusedArguments() {
		first, _, _ := strings.Cut(row.name, " ")
		rowsOf[first]++
	}
	typ := reflect.TypeFor[*Client]()
	checked := 0
	for _, s := range Settings() {
		for _, w := range s.Writes {
			method, ok := typ.MethodByName(string(w.Name))
			if !ok {
				continue // reported by TestTheSettingsTableListsEveryReaderAndWriter
			}
			if method.Type.NumIn() <= 2 { // the receiver and the context
				continue
			}
			checked++
			t.Run(string(w.Name), func(t *testing.T) {
				reason, exempt := noRefusableArgument[w.Name]
				switch {
				case rowsOf[string(w.Name)] == 0 && !exempt:
					t.Errorf("%s takes an argument but has no row in the table of TestEveryRefusedArgumentIsErrInvalid (errors_test.go): add one that refuses an invalid value (a row is named after the writer: `%s <case>`), or name it in noRefusableArgument with the reason there is nothing to refuse", w.Name, w.Name)
				case rowsOf[string(w.Name)] > 0 && exempt:
					t.Errorf("%s has a refused-argument row and is also named in noRefusableArgument (%q): remove the exemption", w.Name, reason)
				}
			})
		}
	}
	if checked < 15 { // a floor against a broken scan, not a count of the writers
		t.Fatalf("checked only %d writers with an argument; the scan is broken", checked)
	}
	for name := range noRefusableArgument {
		t.Run("exempt "+string(name), func(t *testing.T) {
			if _, ok := typ.MethodByName(string(name)); !ok {
				t.Errorf("noRefusableArgument names %s, which Client does not have: remove it", name)
			}
		})
	}
}
