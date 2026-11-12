package main

import (
	"slices"
	"strings"
	"testing"
)

// parse reads one source file into a tree, as the package "p" of the directory "d".
func parse(t *testing.T, file, src string) *tree {
	t.Helper()
	tr := &tree{module: "example.test/lib", nodes: map[string]*node{}, pkgNames: map[string]string{}}
	if err := tr.addFile(file, []byte(src)); err != nil {
		t.Fatalf("addFile: %v", err)
	}
	return tr
}

func TestAddFileReadsEveryKindOfDeclaration(t *testing.T) {
	const src = `package p

type Public struct{ A int }
type private struct{}

func Func(a int) string { return helper() }
func helper() string    { return "" }
func (p *Public) Method() {}
func (p Public) other()   {}
func (p *private) Name()  {}

const (
	One Kind = iota
	Two
	Three
)

const Typed int = 7
var Var, _ = 1, 2
var hidden = Typed

type Kind int

func Bodyless(x int)

type Set[T any] struct{}

func (s *Set[T]) Add(v T) {}
func (s Set[K, V]) Pairs() {}
func (s (Set[T])) Paren()  {}
func (a.B) Odd()           {}
const Missing int
`
	tr := parse(t, "d/p.go", src)
	want := []struct {
		name, recv, kind string
		exported         bool
		sigHas           string
	}{
		{"Public", "", kindType, true, "struct"},
		{"Func", "", kindFunc, true, "func(a int) string"},
		{"helper", "", kindFunc, false, "func() string"},
		{"Method", "Public", kindMethod, true, "(*Public) func()"},
		{"other", "Public", kindMethod, false, "(Public) func()"},
		{"Name", "private", kindMethod, false, "(*private) func()"},
		{"One", "", kindConst, true, "Kind = iota iota=0"},
		{"Two", "", kindConst, true, "Kind = iota iota=1"},
		{"Three", "", kindConst, true, "Kind = iota iota=2"},
		{"Typed", "", kindConst, true, "int = 7"},
		{"Var", "", kindVar, true, " = 1"},
		{"hidden", "", kindVar, false, " = Typed"},
		{"Kind", "", kindType, true, "Kind int"},
		{"Bodyless", "", kindFunc, true, "func(x int)"},
		{"Add", "Set", kindMethod, true, "(*Set[T]) func(v T)"},
		{"Pairs", "Set", kindMethod, true, "(Set[K, V]) func()"},
		{"Paren", "Set", kindMethod, true, "((Set[T])) func()"},
		{"Odd", "", kindMethod, false, "(a.B) func()"},
		{"Missing", "", kindConst, true, "int"},
	}
	for _, w := range want {
		t.Run(w.name, func(t *testing.T) {
			n := tr.nodes[nodeKey("d", w.recv, w.name)]
			if n == nil {
				t.Fatalf("no node %s; have %v", nodeKey("d", w.recv, w.name), keysOf(tr))
			}
			if n.kind != w.kind || n.exported != w.exported || !strings.Contains(n.sig, w.sigHas) {
				t.Errorf("got kind=%s exported=%v sig=%q; want %s %v containing %q", n.kind, n.exported, n.sig, w.kind, w.exported, w.sigHas)
			}
			if n.dir != "d" || n.pkg != "p" || n.file != "d/p.go" {
				t.Errorf("position: dir=%q pkg=%q file=%q", n.dir, n.pkg, n.file)
			}
		})
	}
	if _, ok := tr.nodes[nodeKey("d", "", "private")]; ok {
		t.Error("an unexported type is a node")
	}
	if _, ok := tr.nodes[nodeKey("d", "", "_")]; ok {
		t.Error("a blank identifier is a node")
	}
	if got := tr.pkgNames["d"]; got != "p" {
		t.Errorf("pkgNames = %q", got)
	}
}

func keysOf(tr *tree) []string {
	var keys []string
	for k := range tr.nodes {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func TestSymbolNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    node
		want string
	}{
		{"a function", node{pkg: "escp", name: "Dial"}, "escp.Dial"},
		{"a method", node{pkg: "escp", recv: "Client", name: "Close"}, "escp.Client.Close"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.n.symbol(); got != tc.want {
				t.Errorf("symbol = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAddFileRefusesASyntaxError(t *testing.T) {
	tr := &tree{nodes: map[string]*node{}, pkgNames: map[string]string{}}
	err := tr.addFile("p.go", []byte("package p\nfunc ("))
	if err == nil || !strings.Contains(err.Error(), "parse p.go") {
		t.Errorf("err = %v", err)
	}
}

func TestADeclarationInTwoFilesIsOneNodeThatChangesWithEither(t *testing.T) {
	tr := parse(t, "p_linux.go", "package p\nfunc F() int { return a() }\n")
	if err := tr.addFile("p_windows.go", []byte("package p\nfunc F() int { return b() }\n")); err != nil {
		t.Fatal(err)
	}
	n := tr.nodes[nodeKey(".", "", "F")]
	if !strings.Contains(n.body, "a()") || !strings.Contains(n.body, "b()") {
		t.Errorf("body = %q", n.body)
	}
	if !slices.Contains(n.calls.idents, "a") || !slices.Contains(n.calls.idents, "b") {
		t.Errorf("idents = %v", n.calls.idents)
	}
	if !slices.Equal(n.calls.selectors, nil) {
		t.Errorf("selectors = %v", n.calls.selectors)
	}
}

func TestIsSurfaceFile(t *testing.T) {
	for _, tc := range []struct {
		file string
		want bool
	}{
		{"client.go", true},
		{"model/ip.go", true},
		{"escptest/switch.go", true},
		{"client_test.go", false},
		{"README.md", false},
		{"internal/wire/packet.go", false},
		{"devtools/subtests/main.go", false},
		{"examples/consumer/main.go", false},
		{"scripts/x.go", false},
		{"model/testdata/x.go", false},
		{".github/x.go", false},
		{"build/x.go", false},
	} {
		t.Run(tc.file, func(t *testing.T) {
			if got := isSurfaceFile(tc.file); got != tc.want {
				t.Errorf("isSurfaceFile(%q) = %v", tc.file, got)
			}
		})
	}
}

func TestCollectRefsKeepsTheNameAfterADotApart(t *testing.T) {
	tr := parse(t, "p.go", "package p\nfunc F(x T) { helper(); x.Method(); y := Z; _ = y.Field }\n")
	n := tr.nodes[nodeKey(".", "", "F")]
	for _, id := range []string{"helper", "x", "Z", "T"} { // subtests: the names one function must mention
		if !slices.Contains(n.calls.idents, id) {
			t.Errorf("idents %v lack %q", n.calls.idents, id)
		}
	}
	for _, id := range []string{"Method", "Field"} { // subtests: the names one function must mention after a dot
		if slices.Contains(n.calls.idents, id) || !slices.Contains(n.calls.selectors, id) {
			t.Errorf("%q must be a selector only: idents %v selectors %v", id, n.calls.idents, n.calls.selectors)
		}
	}
}

func TestModuleOf(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"plain", "module example.test/lib\n\ngo 1.26\n", "example.test/lib"},
		{"quoted", "go 1.26\nmodule \"example.test/lib\"\n", "example.test/lib"},
		{"none", "go 1.26\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := moduleOf(tc.in); got != tc.want {
				t.Errorf("moduleOf = %q", got)
			}
		})
	}
}
