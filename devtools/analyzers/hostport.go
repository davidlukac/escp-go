package main

import (
	"go/ast"
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
// with "no reply from switch" beside a process that held the port). A literal that aims a client at the
// fake, which sets SwitchPort because the fake has a port of its own, says nothing of HostPort, and the
// next test copies it. The fix is a port from the test allocator (`HostPort: escptest.FreeUDPPort(t)`).
//
// It reports, in a _test.go file, a composite literal of the configuration type that sets SwitchPort and
// not HostPort, unless
//
//   - the variable it is assigned to has its HostPort assigned in the same function (`cfg.HostPort = ...`), or
//
//   - it is handed (itself, or through that variable) to a function of the package that gives the client its
//     port: one that assigns the HostPort of a parameter of the configuration type (`newBoundClient`,
//     which takes the port from `escptest.FreeUDPPort`), or
//
//   - the literal carries
//
//     // hostport: <reason>
//
//     on its line, on the line above it, or in the function's doc comment (the whole function). Write why the
//
// test must bind the real port (it tests the default), or where the port is set instead. An annotation
// without a reason is itself reported. Configuration is by flag:
//
//	-hostport.type  regular expression of the qualified name of the configuration type (default: the
//	                escp-go Config)
//
// What it does not see: a literal that sets neither field (it aims at a real switch, which is the live
// tests' case), one built from another value (`cfg := other`), a HostPort set to the constant 0, a helper
// of another package that sets the port, and a literal that a function returns to a caller that sets it.
var Hostport = newHostport()

func newHostport() *analysis.Analyzer {
	typ := defaultHostportType
	a := &analysis.Analyzer{
		Name: "hostport",
		Doc:  "reports a Config literal in a test that sets SwitchPort and not HostPort, and has no `// hostport: <reason>` comment",
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
	// of a parameter of the configuration type.
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
			for obj := range s.portAssigned(fn.Body) {
				if params[obj] {
					owners[s.pass.TypesInfo.Defs[fn.Name]] = true
				}
			}
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

// portAssigned is the variables whose HostPort field is assigned somewhere under n (`cfg.HostPort = p`).
func (s *portScan) portAssigned(n ast.Node) map[types.Object]bool {
	assigned := map[types.Object]bool{}
	ast.Inspect(n, func(n ast.Node) bool {
		if as, ok := n.(*ast.AssignStmt); ok {
			for _, lhs := range as.Lhs {
				if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == hostportHostField {
					if id, ok := sel.X.(*ast.Ident); ok {
						assigned[s.pass.TypesInfo.ObjectOf(id)] = true
					}
				}
			}
		}
		return true
	})
	return assigned
}

// scan reports the literals under decl that set SwitchPort and leave the reply port to chance.
func (s *portScan) scan(decl ast.Decl) {
	// owned maps a literal to the variable it initializes (`cfg := Config{...}`, `cfg = &Config{...}`).
	owned := map[*ast.CompositeLit]types.Object{}
	// excused holds the literals and the variables that are handed to a function that gives the port.
	excused := map[any]bool{}
	assigned := s.portAssigned(decl)
	ast.Inspect(decl, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.AssignStmt:
			if len(n.Lhs) == len(n.Rhs) {
				for i, lhs := range n.Lhs {
					if id, ok := lhs.(*ast.Ident); ok && literalOf(n.Rhs[i]) != nil {
						owned[literalOf(n.Rhs[i])] = s.pass.TypesInfo.ObjectOf(id)
					}
				}
			}
		case *ast.CallExpr:
			s.markHandedOver(n, excused)
		}
		return true
	})
	ast.Inspect(decl, func(n ast.Node) bool {
		if lit, ok := n.(*ast.CompositeLit); ok {
			v := owned[lit]
			if !excused[lit] && (v == nil || !excused[v] && !assigned[v]) {
				s.literal(lit)
			}
		}
		return true
	})
}

// markHandedOver records the arguments of a call of a function that gives a client its port.
func (s *portScan) markHandedOver(call *ast.CallExpr, excused map[any]bool) {
	var id *ast.Ident
	switch f := call.Fun.(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	default:
		return
	}
	if !s.owners[s.pass.TypesInfo.Uses[id]] {
		return
	}
	for _, arg := range call.Args {
		if lit := literalOf(arg); lit != nil {
			excused[lit] = true
		} else if v, ok := arg.(*ast.Ident); ok {
			excused[s.pass.TypesInfo.ObjectOf(v)] = true
		}
	}
}

func (s *portScan) literal(lit *ast.CompositeLit) {
	if !s.isConfigType(s.pass.TypesInfo.TypeOf(lit)) {
		return
	}
	set := fieldsSet(lit)
	if !set[hostportSwitchField] || set[hostportHostField] || s.notes.near(lit.Pos()) {
		return
	}
	s.pass.Reportf(lit.Pos(), "a test client aimed at the fake (SwitchPort is set) binds the real reply port 29809 and shares it with every other client on the host: set `HostPort: escptest.FreeUDPPort(t)`, or explain the real port with `// hostport: <reason>`")
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

// fieldsSet is the names of the fields a keyed literal sets.
func fieldsSet(lit *ast.CompositeLit) map[string]bool {
	set := map[string]bool{}
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok {
				set[id.Name] = true
			}
		}
	}
	return set
}
