package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
)

// site is a place in the consumer that uses a symbol.
type site struct {
	file   string
	line   int
	byName bool // a method found by its name alone: the receiver's type is not known
}

// String is the site as a reader sees it; a "~" says the match is by name.
func (s site) String() string {
	prefix := ""
	if s.byName {
		prefix = "~"
	}
	return fmt.Sprintf("%s%s:%d", prefix, s.file, s.line)
}

// usage is where a consumer uses the library.
type usage struct {
	files   int               // Go files read
	library int               // of them, files that import the library
	exact   map[string][]site // declaration key -> sites that name it through an import
	methods map[string][]site // method name -> sites that call something of that name
}

// sitesOf lists the places that use n: those that name it through the import, and for a method those
// that call a method of its name.
func (u *usage) sitesOf(n *node) []site {
	if n.recv != "" {
		return u.methods[n.name]
	}
	return u.exact[n.key()]
}

// scanConsumer reads the Go files under dir (tests included; hidden directories, vendor and testdata
// left out) and records each use of the library of t. It refuses a directory none of whose files import
// the library, because a mistyped path would otherwise read as "nothing is used".
func scanConsumer(dir string, t *tree) (*usage, error) {
	u := &usage{exact: map[string][]site{}, methods: map[string][]site{}}
	fsys := os.DirFS(dir)
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && p != "." && skipConsumerDir(d.Name()):
			return fs.SkipDir
		case d.IsDir() || !strings.HasSuffix(p, ".go"):
			return nil
		}
		src, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		return u.addFile(p, src, t)
	})
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	if u.files == 0 {
		return nil, fmt.Errorf("no Go file under %s", dir)
	}
	if u.library == 0 {
		return nil, fmt.Errorf("no Go file under %s imports %s", dir, t.module)
	}
	return u, nil
}

func skipConsumerDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata"
}

// addFile records the uses of the library in one file; file is its slash-separated path under the root.
func (u *usage) addFile(file string, src []byte, t *tree) error {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		return err
	}
	u.files++
	local := libraryImports(f, t)
	if len(local) > 0 {
		u.library++
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		at := site{file: file, line: fset.Position(sel.Sel.Pos()).Line}
		if id, ok := sel.X.(*ast.Ident); ok && local[id.Name] != "" {
			key := nodeKey(local[id.Name], "", sel.Sel.Name)
			u.exact[key] = append(u.exact[key], at)
			return true
		}
		at.byName = true
		u.methods[sel.Sel.Name] = append(u.methods[sel.Sel.Name], at)
		return true
	})
	return nil
}

// libraryImports maps the local name of each import of the library's packages in f to the package's
// directory in the library.
func libraryImports(f *ast.File, t *tree) map[string]string {
	local := map[string]string{}
	for _, imp := range f.Imports {
		p := strings.Trim(imp.Path.Value, "\"`")
		dir, ok := libraryDir(p, t.module)
		if !ok {
			continue
		}
		name := path.Base(p)
		if known := t.pkgNames[dir]; known != "" {
			name = known
		}
		if imp.Name != nil {
			name = imp.Name.Name
		}
		local[name] = dir // a dot import is read as an import, and its uses are not found: nothing is qualified
	}
	return local
}

// libraryDir maps an import path to a directory of the library, if it is one of its packages.
func libraryDir(importPath, module string) (string, bool) {
	switch {
	case importPath == module:
		return rootDir, true
	case strings.HasPrefix(importPath, module+"/"):
		return strings.TrimPrefix(importPath, module+"/"), true
	}
	return "", false
}

// describeSites words the sites of a symbol for the table: how many, and the first few (all of them
// when limit is 0). Sites in the consumer's tests come last.
func describeSites(sites []site, limit int) string {
	if len(sites) == 0 {
		return "-"
	}
	sorted := append([]site(nil), sites...)
	sort.SliceStable(sorted, func(i, j int) bool {
		ti, tj := strings.HasSuffix(sorted[i].file, "_test.go"), strings.HasSuffix(sorted[j].file, "_test.go")
		if ti != tj {
			return !ti
		}
		if sorted[i].file != sorted[j].file {
			return sorted[i].file < sorted[j].file
		}
		return sorted[i].line < sorted[j].line
	})
	shown := sorted
	if limit > 0 && len(shown) > limit {
		shown = shown[:limit]
	}
	parts := make([]string, len(shown))
	for i, s := range shown {
		parts[i] = s.String()
	}
	text := fmt.Sprintf("%d: %s", len(sorted), strings.Join(parts, ", "))
	if len(shown) < len(sorted) {
		text += ", ..."
	}
	return text
}
