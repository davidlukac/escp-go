package main

import (
	"go/ast"
	"go/token"
)

// addDecls records the exported identifiers of a source file, and which of them state a guarantee.
func (p *pkgDir) addDecls(fset *token.FileSet, rel string, f *ast.File) {
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			p.addFunc(fset, rel, d)
		case *ast.GenDecl:
			p.addGen(fset, rel, d)
		}
	}
}

// record notes one exported identifier and, when its doc comment states a guarantee, a finding.
func (p *pkgDir) record(fset *token.FileSet, rel, name string, pos token.Pos, doc *ast.CommentGroup) {
	p.exported[name] = true
	if s := sentenceOf(doc); s != "" {
		p.findings = append(p.findings, Finding{File: rel, Line: fset.Position(pos).Line, Name: name, Sentence: s})
	}
}

// addFunc records an exported function, or an exported method of an exported type.
func (p *pkgDir) addFunc(fset *token.FileSet, rel string, d *ast.FuncDecl) {
	if !d.Name.IsExported() {
		return
	}
	name := d.Name.Name
	if d.Recv != nil {
		recv := receiverName(d.Recv)
		if !ast.IsExported(recv) {
			return
		}
		name = recv + "." + name
	}
	p.record(fset, rel, name, d.Name.Pos(), d.Doc)
}

// receiverName is the type name of a method's receiver, without the pointer or the type parameters.
func receiverName(recv *ast.FieldList) string {
	if recv == nil || len(recv.List) == 0 {
		return ""
	}
	expr := recv.List[0].Type
	for {
		switch t := expr.(type) {
		case *ast.StarExpr:
			expr = t.X
		case *ast.IndexExpr:
			expr = t.X
		case *ast.IndexListExpr:
			expr = t.X
		case *ast.Ident:
			return t.Name
		default:
			return ""
		}
	}
}

// addGen records the exported types, constants and variables of a declaration. A comment on a group
// of several specs belongs to the group, not to one name, so it is not taken for the doc of each.
func (p *pkgDir) addGen(fset *token.FileSet, rel string, d *ast.GenDecl) {
	for _, spec := range d.Specs {
		doc := specDoc(spec)
		if doc == nil && len(d.Specs) == 1 {
			doc = d.Doc
		}
		switch s := spec.(type) {
		case *ast.TypeSpec:
			if s.Name.IsExported() {
				p.record(fset, rel, s.Name.Name, s.Name.Pos(), doc)
				p.addMembers(fset, rel, s)
			}
		case *ast.ValueSpec:
			for _, n := range s.Names {
				if n.IsExported() {
					p.record(fset, rel, n.Name, n.Pos(), doc)
				}
			}
		}
	}
}

func specDoc(spec ast.Spec) *ast.CommentGroup {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		return s.Doc
	case *ast.ValueSpec:
		return s.Doc
	}
	return nil
}

// addMembers records the exported fields of a struct type and the exported methods of an interface.
func (p *pkgDir) addMembers(fset *token.FileSet, rel string, s *ast.TypeSpec) {
	var fields *ast.FieldList
	switch t := s.Type.(type) {
	case *ast.StructType:
		fields = t.Fields
	case *ast.InterfaceType:
		fields = t.Methods
	}
	if fields == nil {
		return
	}
	for _, f := range fields.List {
		for _, n := range f.Names {
			if n.IsExported() {
				p.record(fset, rel, s.Name.Name+"."+n.Name, n.Pos(), f.Doc)
			}
		}
	}
}
