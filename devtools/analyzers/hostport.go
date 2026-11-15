package main

import (
	"go/ast"
	"go/token"
	"go/types"
	"regexp"

	"golang.org/x/tools/go/analysis"
)

// hostportMarker is the comment that explains a test client that binds the real reply port; the reason
// after it is mandatory.
const hostportMarker = "hostport:"

// Names of the fields the check reads, and the default of its flag: the configuration of the escp-go client.
const (
	hostportSwitchField = "SwitchPort"
	hostportHostField   = "HostPort"
	// defaultHostportType matches the qualified name (import path, a dot, the type name) of the
	// configuration whose literals are checked.
	defaultHostportType = `^github\.com/davidlukac/escp-go\.Config$`
)

// Hostport is the hostport check: a test that aims a client at the fake switch must give the client a
// reply port of its own.
//
// A client binds the UDP port a real switch replies to, 29809, and shares it with every other client on
// the host (the vendor utility, the hardware run of a front end, another `go test` of the same package).
// A unicast reply goes to one of the sockets that share the port, so a test whose client is left on it
// can lose its reply to a stranger, and the stranger loses its to the test (escp-go#237: two tests failed
// with "no reply from switch" beside a process that held the port). A configuration that aims a client at
// the fake sets SwitchPort because the fake has a port of its own, says nothing of HostPort, and the next
// test copies it. The fix is a port from the test allocator (`HostPort: escptest.FreeUDPPort(t)`).
//
// In a _test.go file it reports two shapes:
//
//   - a composite literal of the configuration type that sets SwitchPort and not HostPort, where the literal
//     is an argument, a return value or an element (a literal that initializes a variable is judged as the
//     variable below);
//   - a variable of the configuration type, declared with a literal or with `var`, that is aimed at the
//     fake (its literal sets SwitchPort, or `cfg.SwitchPort = ...` is assigned) and has no port by the time
//     it is first used. A use is the variable as a call argument, a return value, an element of a
//     composite literal, the operand of `&` or `*`, or the right side of an assignment; reading or writing
//     one of its fields is not one. The port is there when its literal sets HostPort, when
//     `cfg.HostPort = <value>` is assigned before the first use (an assignment after the first use does not
//     count: the client was already made), or when the first use is the variable handed to a function that
//     gives the port.
//
// A function of the package gives the port when it assigns the HostPort of a parameter of the configuration
// type from a value that is not a constant (`newBoundClient`, which takes it from `escptest.FreeUDPPort`):
// `cfg.HostPort = 29809` is the shared port again, and `cfg.HostPort = 0` the default. A literal or a
// variable handed to such a function, directly, is excused.
//
// An intended case carries
//
//	// hostport: <reason>
//
// on the line of the report (the literal, or the first `SwitchPort` assignment), on the line above it, or in
// the function's doc comment (the whole function). Write why the test must bind the real port (it tests the
// default), or where the port is set instead. An annotation without a reason is itself reported.
// Configuration is by flag:
//
//	-hostport.type  regular expression of the qualified name of the configuration type (default: the
//	                escp-go Config)
//
// What it does not see: a configuration that sets neither SwitchPort nor HostPort (it aims at a real switch,
// which is the live tests' case), one copied from another value with the port zeroed (`cfg := other;
// cfg.HostPort = 0` is a constant assignment and ignored; `cfg := other` alone is not aimed), a literal with
// `HostPort: 0` written out or with a HostPort variable that is 0 (a table row), a helper of another package
// that sets the port, an order of use that depends on a loop or a closure called later (positions in the
// source are compared), and a literal that a function returns to a caller that sets the port (annotate it).
var Hostport = newHostport()

func newHostport() *analysis.Analyzer {
	typ := defaultHostportType
	a := &analysis.Analyzer{
		Name: "hostport",
		Doc:  "reports a Config in a test that is aimed at the fake (SwitchPort set) and has no HostPort before its first use, and has no `// hostport: <reason>` comment",
	}
	a.Flags.StringVar(&typ, "type", defaultHostportType, "regular expression of the qualified name of the configuration type")
	a.Run = func(pass *analysis.Pass) (any, error) { return runHostport(pass, typ) }
	return a
}

func runHostport(pass *analysis.Pass, typ string) (any, error) {
	re, err := regexp.Compile(typ)
	if err != nil {
		return nil, err
	}
	s := &portScan{pass: pass, config: re}
	s.owners = s.portOwners()
	for _, f := range pass.Files {
		if !isTestFile(pass, f) {
			continue
		}
		s.notes = readAnnotations(pass, f, hostportMarker)
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && docHasMarker(fn.Doc, hostportMarker) {
				continue
			}
			s.scan(decl)
		}
	}
	return nil, nil
}

// portScan is the state of one run over one package; notes is that of the file being walked.
type portScan struct {
	pass   *analysis.Pass
	config *regexp.Regexp
	notes  *annotations
	// owners holds the functions of the package that give a client its port: they assign the HostPort
	// of a parameter of the configuration type from a value that is not a constant.
	owners map[types.Object]bool
}

// portOwners finds the functions of the package, in every file, that assign `<parameter>.HostPort`.
func (s *portScan) portOwners() map[types.Object]bool {
	owners := map[types.Object]bool{}
	for _, f := range s.pass.Files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			params := s.configParams(fn)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if as, ok := n.(*ast.AssignStmt); ok {
					for _, id := range s.portAssigned(as) {
						if params[s.pass.TypesInfo.ObjectOf(id)] {
							owners[s.pass.TypesInfo.Defs[fn.Name]] = true
						}
					}
				}
				return true
			})
		}
	}
	return owners
}

// configParams is the parameters of fn that are of the configuration type (or a pointer to it).
func (s *portScan) configParams(fn *ast.FuncDecl) map[types.Object]bool {
	params := map[types.Object]bool{}
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			if obj := s.pass.TypesInfo.Defs[name]; obj != nil && s.isConfigType(obj.Type()) {
				params[obj] = true
			}
		}
	}
	return params
}

// portAssigned is the variables whose HostPort field the statement assigns from a value that is not a
// constant (`cfg.HostPort = p`).
func (s *portScan) portAssigned(as *ast.AssignStmt) []*ast.Ident {
	var ids []*ast.Ident
	for i, lhs := range as.Lhs {
		id := fieldOwner(lhs, hostportHostField)
		if id == nil {
			continue
		}
		if len(as.Lhs) == len(as.Rhs) && s.pass.TypesInfo.Types[as.Rhs[i]].Value != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

// fieldOwner is the variable of an expression `v.<field>`, or nil.
func fieldOwner(e ast.Expr, field string) *ast.Ident {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != field {
		return nil
	}
	id, _ := sel.X.(*ast.Ident)
	return id
}

// cfgVar is what a walk learns of one variable of the configuration type.
type cfgVar struct {
	literal *ast.CompositeLit // the literal it is initialized with, if any
	// switchAt is the first assignment of its SwitchPort, portAt the first position from which it has a
	// port (its literal, the end of an assignment of its HostPort, a call of a function that gives it
	// one), useAt the first use; zero when there is none.
	switchAt, portAt, useAt token.Pos
}

// sooner is the earlier of two positions, where zero means none.
func sooner(a, b token.Pos) token.Pos {
	if a == token.NoPos || b != token.NoPos && b < a {
		return b
	}
	return a
}

// walk collects what scan judges: the variables of the function (or declaration) and the literals.
type walk struct {
	s       *portScan
	vars    map[types.Object]*cfgVar
	lits    map[*ast.CompositeLit]bool // the configuration literals, false once a variable owns one
	excused map[*ast.CompositeLit]bool // the literals handed to a function that gives the port
	skip    map[*ast.Ident]bool        // identifiers that are not a use of the value
}

func (w *walk) variable(id *ast.Ident) *cfgVar {
	obj := w.s.pass.TypesInfo.ObjectOf(id)
	if obj == nil || !w.s.isConfigType(obj.Type()) {
		return nil
	}
	if w.vars[obj] == nil {
		w.vars[obj] = &cfgVar{}
	}
	return w.vars[obj]
}

func (w *walk) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.CompositeLit:
		if w.s.isConfigType(w.s.pass.TypesInfo.TypeOf(n)) {
			if _, seen := w.lits[n]; !seen {
				w.lits[n] = true
			}
		}
	case *ast.SelectorExpr:
		if id, ok := n.X.(*ast.Ident); ok {
			w.skip[id] = true // reading or writing a field is not handing the value on
		}
	case *ast.AssignStmt:
		w.assign(n)
	case *ast.CallExpr:
		w.call(n)
	case *ast.Ident:
		if v := w.variable(n); v != nil && !w.skip[n] && w.s.pass.TypesInfo.Uses[n] != nil {
			v.useAt = sooner(v.useAt, n.Pos())
		}
	}
	return true
}

func (w *walk) assign(as *ast.AssignStmt) {
	for i, lhs := range as.Lhs {
		if id, ok := lhs.(*ast.Ident); ok {
			w.skip[id] = true // the target of an assignment is not a use
			if v := w.variable(id); v != nil && len(as.Lhs) == len(as.Rhs) {
				if lit := literalOf(as.Rhs[i]); lit != nil && v.literal == nil {
					v.literal, w.lits[lit] = lit, false
				}
			}
		}
		if id := fieldOwner(lhs, hostportSwitchField); id != nil {
			if v := w.variable(id); v != nil {
				v.switchAt = sooner(v.switchAt, lhs.Pos())
			}
		}
	}
	for _, id := range w.s.portAssigned(as) {
		if v := w.variable(id); v != nil {
			v.portAt = sooner(v.portAt, as.End())
		}
	}
}

// call notes the arguments of a call of a function that gives a client its port.
func (w *walk) call(call *ast.CallExpr) {
	var id *ast.Ident
	switch f := call.Fun.(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	default:
		return
	}
	if !w.s.owners[w.s.pass.TypesInfo.Uses[id]] {
		return
	}
	for _, arg := range call.Args {
		if lit := literalOf(arg); lit != nil {
			w.excused[lit] = true
		} else if a, ok := arg.(*ast.Ident); ok {
			if v := w.variable(a); v != nil {
				v.portAt = sooner(v.portAt, a.Pos())
			}
		}
	}
}

// scan reports what under decl is aimed at the fake and left the reply port to chance.
func (s *portScan) scan(decl ast.Decl) {
	k := &walk{s: s, vars: map[types.Object]*cfgVar{}, lits: map[*ast.CompositeLit]bool{},
		excused: map[*ast.CompositeLit]bool{}, skip: map[*ast.Ident]bool{}}
	ast.Inspect(decl, k.visit)
	for lit, loose := range k.lits {
		if loose && !k.excused[lit] && sets(lit, hostportSwitchField) && !sets(lit, hostportHostField) {
			s.report(lit.Pos())
		}
	}
	for _, v := range k.vars {
		s.checkVariable(v)
	}
}

// checkVariable reports a variable that is aimed at the fake and has no port before its first use.
func (s *portScan) checkVariable(v *cfgVar) {
	at := v.switchAt
	if v.literal != nil {
		if sets(v.literal, hostportHostField) {
			v.portAt = sooner(v.portAt, v.literal.Pos())
		}
		if sets(v.literal, hostportSwitchField) {
			at = v.literal.Pos()
		}
	}
	if at == token.NoPos || v.portAt != token.NoPos && (v.useAt == token.NoPos || v.portAt <= v.useAt) {
		return
	}
	s.report(at)
}

func (s *portScan) report(at token.Pos) {
	if s.notes.near(at) {
		return
	}
	s.pass.Reportf(at, "a test client aimed at the fake (SwitchPort is set) binds the real reply port 29809 and shares it with every other client on the host: set `HostPort: escptest.FreeUDPPort(t)` before the client is used, or explain the real port with `// hostport: <reason>`")
}

// isConfigType reports whether t is the configuration type, or a pointer to it.
func (s *portScan) isConfigType(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return s.config.MatchString(named.Obj().Pkg().Path() + "." + named.Obj().Name())
}

// literalOf is the composite literal an expression is, with the `&` of a pointer taken off, or nil.
func literalOf(e ast.Expr) *ast.CompositeLit {
	if u, ok := e.(*ast.UnaryExpr); ok {
		e = u.X
	}
	lit, _ := e.(*ast.CompositeLit)
	return lit
}

// sets reports whether a keyed literal sets the field.
func sets(lit *ast.CompositeLit, field string) bool {
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok && id.Name == field {
				return true
			}
		}
	}
	return false
}
