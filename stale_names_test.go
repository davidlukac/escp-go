package escp

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// A document that names an identifier of this module must name one that exists: a rename or a move
// that leaves the old name in a guide, the README or a comment is a defect of class DC-C02, and this
// check finds it. It reads the Markdown files git tracks (the changelog and the Contributor Covenant
// quote history and are left out) and the comments of the Go files git tracks, and fails with the file
// and the line.
//
// What it recognizes as a name of this module, and nothing else:
//
//   - a qualified name: the package (`escp`, `model`, `safety` or `escptest`), a period and the name,
//     optionally with a member (`escp.Client.Close`): the name must exist in that package, and the
//     member in the type;
//   - a member of a type of the module written without its package, `Client.Close` or `(*Client).Close`:
//     when the type exists in one of the four packages, the member must exist in it (a method, an
//     exported field, a method of an interface, one promoted from an embedded type).
//
// A qualified name of another package (`slog.Logger`, `net.Conn`), a type or a name that is not a
// type of this module (a type of the provider or of the standard library), and a bare name without a
// type or a package are not looked at: nothing says which module they belong to. In Markdown only the
// inline code spans of one line are read (a fenced block is compiled by another test or is not Go); in
// a Go comment the whole text is read. A line is exempted in place with a marker that carries a reason:
// the word "stalenames", a colon, and the reason (in Markdown inside an HTML comment); a marker without
// a reason fails.

// stalePackages are the packages of the module whose exported names the documents may use. The key is
// the package name as a document writes it, the value the directory.
var stalePackages = map[string]string{"escp": ".", "model": "model", "safety": "safety", "escptest": "escptest"}

// staleQualified matches a package-qualified name with an optional member, and staleMember matches
// Type.Member and (*Type).Member when no package qualifies it.
var (
	staleQualified = regexp.MustCompile(`\b(escp|model|safety|escptest)\.([A-Z][A-Za-z0-9_]*)(?:\.([A-Z][A-Za-z0-9_]*))?`)
	staleMember    = regexp.MustCompile(`(?:^|[^\w.])(?:\(\*)?([A-Z][A-Za-z0-9_]*)\)?\.([A-Z][A-Za-z0-9_]*)`)
	staleMarker    = regexp.MustCompile(`stalenames:(.*)$`)
)

// staleMarkerEnd closes the HTML comment a Markdown marker is written in.
const staleMarkerEnd = "-->"

// typeRef names a type of one package of the module.
type typeRef struct{ pkg, name string }

// nameIndex is the exported surface of the module's packages: what a document may name.
type nameIndex struct {
	top      map[string]map[string]bool  // package -> exported top-level name
	types    map[string]map[string]bool  // package -> exported type name
	members  map[typeRef]map[string]bool // type -> its own exported members
	embedded map[typeRef][]typeRef       // type -> the types whose members it gets
	byName   map[string][]typeRef        // exported type name -> every package that has it
	resolved int                         // names that were looked up and found, for the sanity check
}

func newNameIndex() *nameIndex {
	return &nameIndex{
		top:      map[string]map[string]bool{},
		types:    map[string]map[string]bool{},
		members:  map[typeRef]map[string]bool{},
		embedded: map[typeRef][]typeRef{},
		byName:   map[string][]typeRef{},
	}
}

func (x *nameIndex) addMember(ref typeRef, name string) {
	if x.members[ref] == nil {
		x.members[ref] = map[string]bool{}
	}
	x.members[ref][name] = true
}

func (x *nameIndex) addTop(pkg, name string) {
	if x.top[pkg] == nil {
		x.top[pkg] = map[string]bool{}
	}
	x.top[pkg][name] = true
}

// baseTypeName is the identifier a receiver or embedded field type names: T, *T, T[P].
func baseTypeName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.StarExpr:
		return baseTypeName(v.X)
	case *ast.IndexExpr:
		return baseTypeName(v.X)
	case *ast.IndexListExpr:
		return baseTypeName(v.X)
	}
	return ""
}

// embeddedRef resolves an embedded type or an alias target to a type of the module, if it is one.
func embeddedRef(pkg string, e ast.Expr) (typeRef, bool) {
	if sel, ok := e.(*ast.SelectorExpr); ok {
		if q, ok := sel.X.(*ast.Ident); ok {
			if _, known := stalePackages[q.Name]; known {
				return typeRef{q.Name, sel.Sel.Name}, true
			}
		}
		return typeRef{}, false
	}
	if name := baseTypeName(e); name != "" {
		return typeRef{pkg, name}, true
	}
	return typeRef{}, false
}

// addFile records the exported surface of one parsed file of package pkg.
func (x *nameIndex) addFile(pkg string, f *ast.File) {
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			x.addFunc(pkg, d)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				x.addSpec(pkg, spec)
			}
		}
	}
}

func (x *nameIndex) addFunc(pkg string, d *ast.FuncDecl) {
	if !d.Name.IsExported() {
		return
	}
	if d.Recv == nil || len(d.Recv.List) != 1 {
		x.addTop(pkg, d.Name.Name)
		return
	}
	if recv := baseTypeName(d.Recv.List[0].Type); recv != "" {
		x.addMember(typeRef{pkg, recv}, d.Name.Name)
	}
}

func (x *nameIndex) addSpec(pkg string, spec ast.Spec) {
	switch s := spec.(type) {
	case *ast.ValueSpec:
		for _, n := range s.Names {
			if n.IsExported() {
				x.addTop(pkg, n.Name)
			}
		}
	case *ast.TypeSpec:
		if !s.Name.IsExported() {
			return
		}
		ref := typeRef{pkg, s.Name.Name}
		x.addTop(pkg, s.Name.Name)
		if x.types[pkg] == nil {
			x.types[pkg] = map[string]bool{}
		}
		x.types[pkg][s.Name.Name] = true
		x.byName[s.Name.Name] = append(x.byName[s.Name.Name], ref)
		if s.Assign.IsValid() {
			if target, ok := embeddedRef(pkg, s.Type); ok {
				x.embedded[ref] = append(x.embedded[ref], target)
			}
		}
		x.addTypeBody(ref, s.Type)
	}
}

// addTypeBody records the fields of a struct and the methods of an interface, and the embedded types
// of both.
func (x *nameIndex) addTypeBody(ref typeRef, body ast.Expr) {
	var fields *ast.FieldList
	switch t := body.(type) {
	case *ast.StructType:
		fields = t.Fields
	case *ast.InterfaceType:
		fields = t.Methods
	default:
		return
	}
	for _, field := range fields.List {
		if len(field.Names) == 0 {
			if target, ok := embeddedRef(ref.pkg, field.Type); ok {
				x.embedded[ref] = append(x.embedded[ref], target)
				if ast.IsExported(target.name) {
					x.addMember(ref, target.name)
				}
			}
			continue
		}
		for _, n := range field.Names {
			if n.IsExported() {
				x.addMember(ref, n.Name)
			}
		}
	}
}

// hasMember reports whether the type has the member itself or through an embedded type. seen stops a
// cycle of embeddings.
func (x *nameIndex) hasMember(ref typeRef, member string, seen map[typeRef]bool) bool {
	if seen[ref] {
		return false
	}
	seen[ref] = true
	if x.members[ref][member] {
		return true
	}
	for _, e := range x.embedded[ref] {
		if x.hasMember(e, member, seen) {
			return true
		}
	}
	return false
}

// loadNameIndex parses the non-test Go files of the module's four public packages.
func loadNameIndex(t *testing.T) *nameIndex {
	t.Helper()
	x := newNameIndex()
	for pkg, dir := range stalePackages {
		paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range paths {
			if strings.HasSuffix(p, "_test.go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), p, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			x.addFile(pkg, f)
		}
	}
	return x
}

// staleProblems returns what is wrong with the names one line of text uses: each element is a short
// description. text is what to read for names (a code span, or a comment line).
func (x *nameIndex) staleProblems(text string) []string {
	var problems []string
	for _, m := range staleQualified.FindAllStringSubmatch(text, -1) {
		pkg, name, member := m[1], m[2], m[3]
		switch {
		case !x.top[pkg][name]:
			problems = append(problems, pkg+"."+name+" does not exist")
		case member == "":
			x.resolved++
		case x.types[pkg][name] && !x.hasMember(typeRef{pkg, name}, member, map[typeRef]bool{}):
			problems = append(problems, pkg+"."+name+"."+member+" does not exist ("+name+" has no such member)")
		default:
			x.resolved++
		}
	}
	// What a qualified name consumed is not read again as Type.Member.
	rest := staleQualified.ReplaceAllString(text, " ")
	for _, m := range staleMember.FindAllStringSubmatch(rest, -1) {
		refs := x.byName[m[1]]
		if len(refs) == 0 {
			continue
		}
		found := false
		for _, ref := range refs {
			found = found || x.hasMember(ref, m[2], map[typeRef]bool{})
		}
		if found {
			x.resolved++
			continue
		}
		problems = append(problems, m[1]+"."+m[2]+" does not exist ("+m[1]+" has no such member)")
	}
	return problems
}

// staleLine checks one line: an exemption with a reason skips it, one without a reason is a problem of
// its own. text is the whole line; names is the part of it that is read for names.
func (x *nameIndex) staleLine(path string, line int, text, names string) []string {
	where := path + ":" + strconv.Itoa(line) + ": "
	// A marker written in a code span is a description of the marker, not an exemption.
	_, outside := splitSpans(text)
	if m := staleMarker.FindStringSubmatch(outside); m != nil {
		if strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(m[1]), staleMarkerEnd)) == "" {
			return []string{where + "an exemption without a reason (write the reason after the colon)"}
		}
		return nil
	}
	var hits []string
	for _, p := range x.staleProblems(names) {
		hits = append(hits, where+p)
	}
	return hits
}

// splitSpans returns the inline code spans of one line of Markdown, without their backticks, and the
// rest of the line.
func splitSpans(line string) (spans []string, rest string) {
	var outside strings.Builder
	for i := 0; i < len(line); {
		if line[i] != '`' {
			outside.WriteByte(line[i])
			i++
			continue
		}
		j := i
		for j < len(line) && line[j] == '`' {
			j++
		}
		fence := line[i:j]
		end := strings.Index(line[j:], fence)
		if end < 0 {
			outside.WriteString(fence)
			i = j
			continue
		}
		spans = append(spans, line[j:j+end])
		outside.WriteByte(' ')
		i = j + end + len(fence)
	}
	return spans, outside.String()
}

// staleMarkdown checks the inline code spans of a Markdown text, skipping fenced blocks.
func (x *nameIndex) staleMarkdown(path, text string) []string {
	var hits []string
	fenced := false
	for i, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		spans, _ := splitSpans(line)
		hits = append(hits, x.staleLine(path, i+1, line, strings.Join(spans, "\n"))...)
	}
	return hits
}

// staleGoComments checks the text of every comment of a parsed Go file.
func (x *nameIndex) staleGoComments(fset *token.FileSet, path string, f *ast.File) []string {
	var hits []string
	for _, group := range f.Comments {
		for _, c := range group.List {
			first := fset.Position(c.Slash).Line
			for i, line := range strings.Split(c.Text, "\n") {
				hits = append(hits, x.staleLine(path, first+i, line, line)...)
			}
		}
	}
	return hits
}

// staleSources returns the tracked files the check reads: the Markdown documents and the Go files.
func staleSources(t *testing.T) (markdown, goFiles []string) {
	t.Helper()
	for _, path := range publishedFiles(t) {
		slash := filepath.ToSlash(path)
		switch base := filepath.Base(path); {
		case strings.HasPrefix(slash, "devtools/"), strings.HasPrefix(slash, "build/"), strings.Contains(slash, "testdata/"):
		case filepath.Ext(path) == ".md" && base != "CHANGELOG.md" && base != "CODE_OF_CONDUCT.md":
			markdown = append(markdown, path)
		case filepath.Ext(path) == ".go":
			goFiles = append(goFiles, path)
		}
	}
	sort.Strings(markdown)
	sort.Strings(goFiles)
	return markdown, goFiles
}

// Every identifier of the module that a document or a comment names exists: a rename that leaves the
// old name behind fails here, with the file and the line, instead of reaching a reader.
func TestEveryIdentifierTheDocsNameExists(t *testing.T) {
	x := loadNameIndex(t)
	markdown, goFiles := staleSources(t)
	if len(markdown) < 10 || len(goFiles) < 100 {
		t.Fatalf("found %d documents and %d Go files: the list of files is broken", len(markdown), len(goFiles))
	}
	var hits []string
	for _, path := range markdown {
		hits = append(hits, x.staleMarkdown(filepath.ToSlash(path), readText(t, path))...)
	}
	for _, path := range goFiles {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		hits = append(hits, x.staleGoComments(fset, filepath.ToSlash(path), f)...)
	}
	for _, h := range hits {
		t.Error(h)
	}
	if x.resolved < 100 {
		t.Errorf("only %d names were looked up and found: the scan is broken", x.resolved)
	}
}

// The index knows the surface of the real packages: a type, a method, a field, a function, a method
// promoted through an embedded type and a name that is not there.
func TestTheNameIndexKnowsTheRealPackages(t *testing.T) {
	x := loadNameIndex(t)
	tests := []struct {
		name string
		text string
		want int
	}{
		{"a function of the root package", "escp.Dial", 0},
		{"a method written with its package", "escp.Client.Close", 0},
		{"a method written without it", "Client.Close", 0},
		{"a method with a pointer receiver", "(*Client).Close", 0},
		{"a value of the model", "model.PortID", 0},
		{"a function of the fake", "escptest.Start", 0},
		{"a name that is not there", "escp.NoSuchThing", 1},
		{"a method that is not there", "Client.NoSuchMethod", 1},
		{"a name moved to another package", "escp.PortID", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := x.staleProblems(tt.text); len(got) != tt.want {
				t.Errorf("staleProblems(%q) = %v, want %d problems", tt.text, got, tt.want)
			}
		})
	}
}

// fixtureIndex is a module of its own for the rules below: one type with a method and a field, an
// interface that embeds another, a struct that embeds a type, an alias and a variable.
func fixtureIndex(t *testing.T) *nameIndex {
	t.Helper()
	const src = `package escp

type Client struct{ Host string; hidden int }
func (c *Client) Close() error { return nil }
func (c *Client) hide() {}
func Dial() {}
var Default Client

type Reader interface{ Read() }
type ReadCloser interface{ Reader; Close() }
type Wrapper struct{ Client }
type Alias = Client
`
	f, err := parser.ParseFile(token.NewFileSet(), "fixture.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	x := newNameIndex()
	x.addFile("escp", f)
	return x
}

func TestStalenamesRules(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int
	}{
		{"a method that exists", "see `Client.Close`", 0},
		{"a call written with its arguments", "`Client.Close()`", 0},
		{"a field that exists", "`Client.Host`", 0},
		{"a method that was renamed", "`Client.Shutdown`", 1},
		{"an unexported member is not a name", "`Client.hide`", 0},
		{"a method promoted through an embedded struct", "`Wrapper.Close`", 0},
		{"the embedded type as a field", "`Wrapper.Client`", 0},
		{"a method of an embedded interface", "`ReadCloser.Read`", 0},
		{"a member of an alias", "`Alias.Close`", 0},
		{"a qualified function that exists", "`escp.Dial`", 0},
		{"a qualified function that was removed", "`escp.Connect`", 1},
		{"a qualified type with a stale member", "`escp.Client.Shutdown`", 1},
		{"a member of a variable is not checked", "`escp.Default.Anything`", 0},
		{"a package of another module", "`slog.Logger`", 0},
		{"a type of another module", "`Handler.Handle`", 0},
		{"a package of the module not written in a span is prose", "escp.Connect", 0},
		{"two problems in one line", "`escp.Gone` and `Client.Gone`", 2},
		{"a name followed by a period", "use `escp.Dial`.", 0},
		{"a bare name is not looked at", "`Shutdown`", 0},
	}
	x := fixtureIndex(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := x.staleMarkdown("doc.md", tt.text)
			if len(got) != tt.want {
				t.Errorf("staleMarkdown(%q) = %v, want %d problems", tt.text, got, tt.want)
			}
		})
	}
}

func TestStalenamesReportsTheFileAndTheLine(t *testing.T) {
	got := fixtureIndex(t).staleMarkdown("docs/guide.md", "first line\nsecond `Client.Shutdown` line")
	want := "docs/guide.md:2: Client.Shutdown does not exist (Client has no such member)"
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %v, want [%s]", got, want)
	}
}

func TestStalenamesSkipsFencedBlocks(t *testing.T) {
	text := "```go\nclient.Shutdown() // `escp.Gone`\n```\nafter `escp.Gone`\n~~~\n`escp.Gone`\n~~~\n"
	got := fixtureIndex(t).staleMarkdown("doc.md", text)
	if len(got) != 1 || !strings.HasPrefix(got[0], "doc.md:4: ") {
		t.Errorf("got %v, want one problem, on line 4", got)
	}
}

func TestStalenamesExemptionNeedsAReason(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{"an exemption with a reason", "`Client.Gone` <!-- stalenames: the provider has a Client too -->", nil},
		{"the reason is the rest of the line", "`Client.Gone` <!-- stalenames: history of the old name -->", nil},
		{"an exemption without a reason", "`Client.Gone` <!-- stalenames: -->", []string{"doc.md:1: an exemption without a reason (write the reason after the colon)"}},
		{"an exemption with blanks only", "`Client.Gone` <!-- stalenames:   -->", []string{"doc.md:1: an exemption without a reason (write the reason after the colon)"}},
		{"a marker written in a code span is not an exemption", "`Client.Gone` and `<!-- stalenames: reason -->`", []string{"doc.md:1: Client.Gone does not exist (Client has no such member)"}},
		{"an exemption covers its own line only", "`Client.Gone` <!-- stalenames: reason -->\n`Client.Gone`", []string{"doc.md:2: Client.Gone does not exist (Client has no such member)"}},
	}
	x := fixtureIndex(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := x.staleMarkdown("doc.md", tt.text)
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStalenamesReadsGoComments(t *testing.T) {
	const src = `package p

// Close shuts the connection; see escp.Dial and Client.Gone.
func f() {}

/* A block comment
   that names escp.Gone on its second line. */
var v = 1 // stalenames: a comment that names Client.Gone on purpose, for a reason

// stalenames:
var w = 2
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	got := fixtureIndex(t).staleGoComments(fset, "p.go", f)
	want := []string{
		"p.go:3: Client.Gone does not exist (Client has no such member)",
		"p.go:7: escp.Gone does not exist",
		"p.go:10: an exemption without a reason (write the reason after the colon)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestSplitSpansOfALine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string
		rest string
	}{
		{"none", "no code here", nil, "no code here"},
		{"one", "a `b` c", []string{"b"}, "a   c"},
		{"two", "`a` and `b`", []string{"a", "b"}, "  and  "},
		{"double backticks hold a single one", "``a`b``", []string{"a`b"}, " "},
		{"an unclosed span is not one", "a `b", nil, "a `b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, rest := splitSpans(tt.line)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") || rest != tt.rest {
				t.Errorf("splitSpans(%q) = %q, %q, want %q, %q", tt.line, got, rest, tt.want, tt.rest)
			}
		})
	}
}

// The files the check reads are the ones git tracks, minus the history and the checks' own trees.
func TestStalenamesReadsTheRightFiles(t *testing.T) {
	markdown, goFiles := staleSources(t)
	tests := []struct {
		name string
		list []string
		file string
		want bool
	}{
		{"the README is read", markdown, "README.md", true},
		{"the contribution guide is read", markdown, "CONTRIBUTING.md", true},
		{"the protocol notes are read", markdown, "PROTOCOL.md", true},
		{"a guide is read", markdown, filepath.Join("docs", "architecture.md"), true},
		{"the changelog quotes history", markdown, "CHANGELOG.md", false},
		{"the code of conduct is not ours", markdown, "CODE_OF_CONDUCT.md", false},
		{"a Go file is read", goFiles, "client.go", true},
		{"the devtools name their own things", goFiles, filepath.Join("devtools", "analyzers", "annotation.go"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := slices.Contains(tt.list, tt.file); got != tt.want {
				t.Errorf("%s read = %v, want %v", tt.file, got, tt.want)
			}
		})
	}
}
