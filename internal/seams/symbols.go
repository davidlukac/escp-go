package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path"
	"slices"
	"strings"
)

// Kinds of a top-level declaration.
const (
	kindFunc   = "func"
	kindMethod = "method"
	kindType   = "type"
	kindConst  = "const"
	kindVar    = "var"
)

// rootDir is the directory of the root package in a node key.
const rootDir = "."

// node is one top-level declaration of a revision: a function, a method, an exported type, or a
// constant or variable (exported or not, because a changed unexported constant changes the functions
// that read it).
type node struct {
	dir   string // directory of its package, "." for the root package
	pkg   string // package name
	recv  string // the receiver's base type, "" for a package-level declaration
	name  string
	kind  string
	file  string
	sig   string // the printed signature, type or value: what a caller depends on
	body  string // the printed body, "" for a declaration without one
	calls refs   // what the body or the value mentions
	// exported is true when a caller outside the package can name it.
	exported bool
}

// refs are the names a declaration mentions: identifiers (functions, constants and variables of its
// package) and the names after a dot (methods of its package, found by name only, without types).
type refs struct {
	idents    []string
	selectors []string
}

// key identifies a declaration across revisions.
func (n *node) key() string { return nodeKey(n.dir, n.recv, n.name) }

func nodeKey(dir, recv, name string) string { return dir + "|" + recv + "|" + name }

// symbol is the name a person reads: escp.Client.Close, model.ParseMAC.
func (n *node) symbol() string {
	if n.recv == "" {
		return n.pkg + "." + n.name
	}
	return n.pkg + "." + n.recv + "." + n.name
}

// tree is the declarations of one revision.
type tree struct {
	module   string
	nodes    map[string]*node
	pkgNames map[string]string // directory -> package name
}

// excludedDirs hold no part of the exported surface: build tools, tests' data, examples, scripts.
var excludedDirs = map[string]bool{
	"internal": true, "devtools": true, "examples": true, "scripts": true,
	"testdata": true, "build": true, "vendor": true, "docs": true,
}

// isSurfaceFile reports whether a path of the repository is a production Go file of a package a
// consumer can import.
func isSurfaceFile(name string) bool {
	if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
		return false
	}
	for part := range strings.SplitSeq(path.Dir(name), "/") {
		if excludedDirs[part] || (strings.HasPrefix(part, ".") && part != ".") {
			return false
		}
	}
	return true
}

// addFile parses one file and adds its declarations. Two files that declare the same name (the
// per-system variants of one function) are merged, so a change in either one counts.
func (t *tree) addFile(file string, src []byte) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		return fmt.Errorf("parse %s: %w", file, err)
	}
	dir := path.Dir(file)
	t.pkgNames[dir] = f.Name.Name
	for _, d := range f.Decls {
		var found []*node
		switch d := d.(type) {
		case *ast.FuncDecl:
			found = []*node{funcNode(fset, d)}
		case *ast.GenDecl:
			found = genNodes(fset, d)
		}
		for _, n := range found {
			n.dir, n.pkg, n.file = dir, f.Name.Name, file
			t.add(n)
		}
	}
	return nil
}

// add inserts n, merging it into a node of the same key that an earlier file declared.
func (t *tree) add(n *node) {
	old, ok := t.nodes[n.key()]
	if !ok {
		t.nodes[n.key()] = n
		return
	}
	old.sig += "\n" + n.sig
	old.body += "\n" + n.body
	old.calls.idents = append(old.calls.idents, n.calls.idents...)
	old.calls.selectors = append(old.calls.selectors, n.calls.selectors...)
}

// funcNode describes a function or a method declaration.
func funcNode(fset *token.FileSet, d *ast.FuncDecl) *node {
	n := &node{name: d.Name.Name, kind: kindFunc, sig: printNode(fset, d.Type), calls: collectRefs(d)}
	n.exported = ast.IsExported(n.name)
	if d.Recv != nil {
		n.kind = kindMethod
		n.recv = baseTypeName(d.Recv.List[0].Type)
		n.sig = "(" + printNode(fset, d.Recv.List[0].Type) + ") " + n.sig
		n.exported = n.exported && ast.IsExported(n.recv)
	}
	if d.Body != nil {
		n.body = printNode(fset, d.Body)
	}
	return n
}

// baseTypeName is the type a receiver is declared on, without the pointer and the type arguments.
func baseTypeName(e ast.Expr) string {
	for {
		switch t := e.(type) {
		case *ast.StarExpr:
			e = t.X
		case *ast.ParenExpr:
			e = t.X
		case *ast.IndexExpr:
			e = t.X
		case *ast.IndexListExpr:
			e = t.X
		case *ast.Ident:
			return t.Name
		default:
			return "" // not a receiver the compiler accepts
		}
	}
}

// genNodes describes the types, constants and variables of a declaration. An unexported type is not a
// node: nothing outside its package names it, and its methods are nodes of their own.
func genNodes(fset *token.FileSet, d *ast.GenDecl) []*node {
	var out []*node
	var last *ast.ValueSpec // the constant spec whose type and values an implicit one repeats
	for i, s := range d.Specs {
		switch s := s.(type) {
		case *ast.TypeSpec:
			if ast.IsExported(s.Name.Name) {
				out = append(out, &node{name: s.Name.Name, kind: kindType, exported: true, sig: printNode(fset, s)})
			}
		case *ast.ValueSpec:
			if d.Tok == token.CONST && len(s.Values) == 0 && last != nil {
				s = &ast.ValueSpec{Names: s.Names, Type: last.Type, Values: last.Values}
			} else if d.Tok == token.CONST {
				last = s
			}
			out = append(out, valueNodes(fset, s, d.Tok, i)...)
		}
	}
	return out
}

// valueNodes describes the names of one constant or variable spec; index is the spec's position in its
// declaration, which is the value of iota.
func valueNodes(fset *token.FileSet, s *ast.ValueSpec, tok token.Token, index int) []*node {
	var text strings.Builder
	if s.Type != nil {
		text.WriteString(printNode(fset, s.Type))
	}
	calls := refs{}
	for _, v := range s.Values {
		text.WriteString(" = " + printNode(fset, v))
		c := collectRefs(v)
		calls.idents = append(calls.idents, c.idents...)
		calls.selectors = append(calls.selectors, c.selectors...)
	}
	if tok == token.CONST && slices.Contains(calls.idents, "iota") {
		fmt.Fprintf(&text, " iota=%d", index)
	}
	sig := text.String()
	kind := kindVar
	if tok == token.CONST {
		kind = kindConst
	}
	out := make([]*node, 0, len(s.Names))
	for _, id := range s.Names {
		if id.Name == "_" {
			continue
		}
		out = append(out, &node{name: id.Name, kind: kind, exported: ast.IsExported(id.Name), sig: sig, calls: calls})
	}
	return out
}

// collectRefs lists the identifiers and the selector names under root. The name after a dot is not an
// identifier of the package: it is kept apart, because it names a method.
func collectRefs(root ast.Node) refs {
	var r refs
	selected := map[*ast.Ident]bool{}
	ast.Inspect(root, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			selected[n.Sel] = true
			r.selectors = append(r.selectors, n.Sel.Name)
		case *ast.Ident:
			if !selected[n] {
				r.idents = append(r.idents, n.Name)
			}
		}
		return true
	})
	return r
}

// printNode prints a node without its comments, so that only code differences show.
func printNode(fset *token.FileSet, n ast.Node) string {
	var b strings.Builder
	// A strings.Builder does not fail; a node that cannot be printed prints as far as it can.
	_ = printer.Fprint(&b, fset, n)
	return b.String()
}
