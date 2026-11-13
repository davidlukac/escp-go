package main

import (
	"fmt"
	"strings"
	"testing"
)

// treeOf reads files (path -> source) into a tree.
func treeOf(t *testing.T, files map[string]string) *tree {
	t.Helper()
	tr := &tree{module: "example.test/lib", nodes: map[string]*node{}, pkgNames: map[string]string{}}
	for name, src := range files {
		if err := tr.addFile(name, []byte(src)); err != nil {
			t.Fatalf("addFile %s: %v", name, err)
		}
	}
	return tr
}

// rowsOf renders the rows of a comparison as "symbol: what".
func rowsOf(c comparison) []string {
	var out []string
	for _, r := range c.rows {
		out = append(out, fmt.Sprintf("%s: %s", r.node.symbol(), r.what))
	}
	return out
}

func TestCompare(t *testing.T) {
	for _, tc := range []struct {
		name     string
		from, to string
		want     []string
	}{
		{"nothing changed", "func F() {}", "func F() {}", nil},
		{"a comment and the layout are no change", "func F() { a() } // old\nfunc a() {}", "// F does more.\nfunc F() {\n\ta()\n}\nfunc a() {}", nil},
		{"a body", "func F() int { return 1 }", "func F() int { return 2 }", []string{"p.F: body"}},
		{"a signature", "func F() {}", "func F(x int) {}", []string{"p.F: signature"}},
		{"both", "func F() int { return 1 }", "func F(x int) int { return 2 }", []string{"p.F: signature, body"}},
		{"added", "func F() {}", "func F() {}\nfunc G() {}", []string{"p.G: added"}},
		{"removed", "func F() {}\nfunc G() {}", "func F() {}", []string{"p.G: removed"}},
		{"an unexported function added or removed is not listed", "func a() {}", "func b() {}", nil},
		{"a receiver", "type T struct{}\nfunc (t T) M() {}", "type T struct{}\nfunc (t *T) M() {}", []string{"p.T.M: signature"}},
		{"a type", "type T struct{ A int }", "type T struct{ A, B int }", []string{"p.T: type"}},
		{"a constant", "const C = 1", "const C = 2", []string{"p.C: value"}},
		{"a constant in an iota list shifts", "const (\n\tA = iota\n\tB\n)", "const (\n\tA = iota\n\tN\n\tB\n)", []string{"p.B: value", "p.N: added"}},
		{"a constant with an explicit value does not change when another is inserted before it", "const (\n\tA = 1\n\tB = 2\n)", "const (\n\tN = 0\n\tA = 1\n\tB = 2\n)", []string{"p.N: added"}},
		{"a method of an unexported type is not listed", "type t struct{}\nfunc (x t) M() { a() }\nfunc a() {}", "type t struct{}\nfunc (x t) M() { a(); a() }\nfunc a() {}", nil},
		{
			"an unexported helper reaches the exported function that calls it",
			"func F() { h() }\nfunc h() int { return 1 }",
			"func F() { h() }\nfunc h() int { return 2 }",
			[]string{"p.F: via h"},
		},
		{
			"the walk stops at the first changed declaration, and goes on through unchanged ones",
			"func F() { m() }\nfunc m() { g() }\nfunc g() { h() }\nfunc h() int { return 1 }",
			"func F() { m() }\nfunc m() { g() }\nfunc g() { h(); h() }\nfunc h() int { return 2 }",
			[]string{"p.F: via g"},
		},
		{
			"a changed exported function is itself listed, and the ones that call it",
			"func F() { G() }\nfunc G() int { return 1 }\nfunc H() { F() }",
			"func F() { G() }\nfunc G() int { return 2 }\nfunc H() { F() }",
			[]string{"p.F: via G", "p.G: body", "p.H: via G"},
		},
		{
			"two paths to one helper name it once, and several are sorted",
			"func F() { b(); a(); a() }\nfunc a() int { return 1 }\nfunc b() int { return 1 }",
			"func F() { b(); a(); a() }\nfunc a() int { return 2 }\nfunc b() int { return 2 }",
			[]string{"p.F: via a, b"},
		},
		{
			"a changed unexported constant reaches the function that reads it",
			"const limit = 1\nfunc F() int { return limit }",
			"const limit = 2\nfunc F() int { return limit }",
			[]string{"p.F: via limit"},
		},
		{
			"a method call is matched to the one method of that name",
			"type T struct{}\nfunc (t *T) run() int { return 1 }\nfunc F(t *T) { t.run() }",
			"type T struct{}\nfunc (t *T) run() int { return 2 }\nfunc F(t *T) { t.run() }",
			[]string{"p.F: via T.run"},
		},
		{
			"a method name several types declare is matched to the types the caller names",
			"type A struct{}\ntype B struct{}\nfunc (A) check() int { return 1 }\nfunc (B) check() int { return 1 }\nfunc F(a A) { a.check() }\nfunc G(b B) { b.check() }",
			"type A struct{}\ntype B struct{}\nfunc (A) check() int { return 2 }\nfunc (B) check() int { return 1 }\nfunc F(a A) { a.check() }\nfunc G(b B) { b.check() }",
			[]string{"p.F: via A.check"},
		},
		{
			"a method name several types declare reaches a method through its own receiver",
			"type A struct{}\ntype B struct{}\nfunc (A) check() int { return 1 }\nfunc (B) check() int { return 1 }\nfunc (a A) Run() { a.check() }",
			"type A struct{}\ntype B struct{}\nfunc (A) check() int { return 2 }\nfunc (B) check() int { return 1 }\nfunc (a A) Run() { a.check() }",
			[]string{"p.A.Run: via A.check"},
		},
		{
			"a changed type does not reach the functions that name it",
			"type T struct{ A int }\nfunc F(t T) {}",
			"type T struct{ A, B int }\nfunc F(t T) {}",
			[]string{"p.T: type"},
		},
		{
			"a changed exported function that calls a changed helper is listed once, as changed",
			"func F() { h() }\nfunc h() int { return 1 }",
			"func F() { h(); h() }\nfunc h() int { return 2 }",
			[]string{"p.F: body"},
		},
		{
			"a method name one type declares is matched through a field of a type the caller does not name",
			"type T struct{}\ntype P struct{ t T }\nfunc (T) run() int { return 1 }\nfunc F(p *P) { p.t.run() }",
			"type T struct{}\ntype P struct{ t T }\nfunc (T) run() int { return 2 }\nfunc F(p *P) { p.t.run() }",
			[]string{"p.F: via T.run"},
		},
		{"a call that resolves to nothing", "func F() { missing() }", "func F() { missing() }", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			from := treeOf(t, map[string]string{"p.go": "package p\n" + tc.from})
			to := treeOf(t, map[string]string{"p.go": "package p\n" + tc.to})
			got := rowsOf(compare(from, to))
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("rows:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

func TestCompareKeepsPackagesApart(t *testing.T) {
	from := treeOf(t, map[string]string{
		"a/a.go": "package a\nfunc F() { h() }\nfunc h() int { return 1 }",
		"b/b.go": "package b\nfunc F() { h() }\nfunc h() int { return 1 }",
	})
	to := treeOf(t, map[string]string{
		"a/a.go": "package a\nfunc F() { h() }\nfunc h() int { return 2 }",
		"b/b.go": "package b\nfunc F() { h() }\nfunc h() int { return 1 }",
	})
	got := strings.Join(rowsOf(compare(from, to)), "\n")
	if got != "a.F: via h" {
		t.Errorf("rows = %q", got)
	}
}

func TestCompareRecordsTheChangedKeys(t *testing.T) {
	from := treeOf(t, map[string]string{"p.go": "package p\nfunc F() { h() }\nfunc h() int { return 1 }"})
	to := treeOf(t, map[string]string{"p.go": "package p\nfunc F() { h() }\nfunc h() int { return 2 }"})
	c := compare(from, to)
	if len(c.changed) != 1 || !c.changed[nodeKey(".", "", "h")] {
		t.Errorf("changed = %v", c.changed)
	}
}

func TestDescribeNamesTheKindOfChange(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b *node
		want string
	}{
		{"same", &node{kind: kindFunc, sig: "x", body: "1"}, &node{kind: kindFunc, sig: "x", body: "1"}, ""},
		{"signature", &node{kind: kindFunc, sig: "x"}, &node{kind: kindFunc, sig: "y"}, changeSignature},
		{"method", &node{kind: kindMethod, sig: "x"}, &node{kind: kindMethod, sig: "y"}, changeSignature},
		{"value of a constant", &node{kind: kindConst, sig: "x"}, &node{kind: kindConst, sig: "y"}, changeValue},
		{"value of a variable", &node{kind: kindVar, sig: "x"}, &node{kind: kindVar, sig: "y"}, changeValue},
		{"type", &node{kind: kindType, sig: "x"}, &node{kind: kindType, sig: "y"}, changeType},
		{"body", &node{kind: kindFunc, body: "1"}, &node{kind: kindFunc, body: "2"}, changeBody},
		{"both", &node{kind: kindFunc, sig: "x", body: "1"}, &node{kind: kindFunc, sig: "y", body: "2"}, changeSignature + ", " + changeBody},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := describe(tc.a, tc.b); got != tc.want {
				t.Errorf("describe = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHelperName(t *testing.T) {
	if got := helperName(&node{name: "h"}); got != "h" {
		t.Errorf("function: %q", got)
	}
	if got := helperName(&node{recv: "T", name: "run"}); got != "T.run" {
		t.Errorf("method: %q", got)
	}
}

func TestCompareListsTheChangedHelpersAndWhoReachesThem(t *testing.T) {
	from := treeOf(t, map[string]string{"q.go": "package p\nfunc E() { k() }\nfunc k() int { return 1 }\n", "p.go": `package p
func A() { m() }
func B() { m(); other() }
func C() { other() }
func m() { h() }
func h() int { return 1 }
func other() {}
func g() int { return 1 }
func D() { g() }
func Changed() int { return 1 }
func Top() { Changed() }
`})
	to := treeOf(t, map[string]string{"q.go": "package p\nfunc E() { k() }\nfunc k() int { return 2 }\n", "p.go": `package p
func A() { m() }
func B() { m(); other() }
func C() { other() }
func m() { h() }
func h() int { return 2 }
func other() {}
func g() string { return "" }
func D() { g() }
func Changed() int { return 2 }
func Top() { Changed() }
func New() { h() }
`})
	c := compare(from, to)
	var got []string
	for _, h := range c.helpers {
		var users []string
		for _, u := range h.users {
			users = append(users, u.name)
		}
		got = append(got, fmt.Sprintf("%s %s: %s", h.node.name, h.what, strings.Join(users, ",")))
	}
	want := "g signature, body: D\nh body: A,B,New\nk body: E" // p.go before q.go
	if strings.Join(got, "\n") != want {
		t.Errorf("helpers:\n%s\nwant:\n%s", strings.Join(got, "\n"), want)
	}
	// New is added, so it is listed as added and not also as reaching a change.
	rows := strings.Join(rowsOf(c), "\n")
	if strings.Count(rows, "p.New") != 1 || !strings.Contains(rows, "p.New: added") {
		t.Errorf("rows:\n%s", rows)
	}
}
