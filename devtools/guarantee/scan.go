package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// skipDirs are directories the scan does not enter: version control, build output, fixtures, the
// tools themselves and the example programs (the guarantees of the library are the ones its packages
// state).
var skipDirs = map[string]bool{".git": true, "build": true, "testdata": true, "vendor": true, "devtools": true, "examples": true}

// markerLine is a marker comment of a test: `// guarantee: Name[, Name...]`.
var markerLine = regexp.MustCompile(`^//\s*guarantee:\s*(.+)$`)

// maxSentence is the longest sentence the report prints before it cuts it.
const maxSentence = 140

// promises are the wordings of a guarantee. They are patterns over one sentence of a doc comment, in
// lower case, with the spaces of the comment folded.
var promises = []*regexp.Regexp{
	regexp.MustCompile(`\bnever\b`),
	regexp.MustCompile(`\b(sends?|sent|writes?|written) nothing\b|\bnothing is (sent|written)\b`),
	regexp.MustCompile(`\bat most\b|\bat least once\b|\bexactly once\b|\bonly once\b`),
	regexp.MustCompile(`\bsafe (for|to) (use |call(ing)? )?(by |from )?(concurrent|multiple|several|any number)|\bconcurrent(ly)? (use|calls?)\b|\bgoroutine[- ]safe\b|\bsafe for concurrent\b`),
	regexp.MustCompile(`\bwithout (sending|writing|blocking|panicking|touching|changing|opening)\b`),
	regexp.MustCompile(`\bdoes not (send|write|block|panic|retry|touch|modify|open)\b`),
	regexp.MustCompile(`\bguarantee[sd]?\b|\bno more than\b|\bcannot (panic|block|hang)\b`),
}

// notPromises are wordings that match a pattern above and are a statement of fact, not a promise
// ("never tried", "has never been configured").
var notPromises = regexp.MustCompile(`\bnever (been|tried)\b`)

// Finding is an exported identifier whose doc comment states a guarantee.
type Finding struct {
	File     string // relative to the root, slash-separated
	Line     int
	Name     string // `Dial`, or `Client.Close` for a method or a field
	Sentence string
}

// Marker is one name of a `// guarantee:` comment of a test.
type Marker struct {
	File string
	Line int
	Name string
	// handed is set when the name was `<dir>.<Name>` and the identifier it names belongs to the
	// package in <dir>, so it is that package's marker and no stale one.
	handed bool
}

// Result is what a scan found.
type Result struct {
	Matched   []Finding // a guarantee with a marker
	Unmatched []Finding // a guarantee without one
	// Stale are marker names that no exported identifier of the package has.
	Stale []Marker
}

// HasFindings reports whether there is anything to act on.
func (r Result) HasFindings() bool { return len(r.Unmatched) > 0 || len(r.Stale) > 0 }

// Print writes the report: one line per finding, then the count.
func (r Result) Print(w io.Writer) {
	for _, f := range r.Unmatched {
		_, _ = fmt.Fprintf(w, "%s:%d: %s: %s (no test carries `// guarantee: %s`)\n", f.File, f.Line, f.Name, f.Sentence, f.Name)
	}
	for _, m := range r.Stale {
		_, _ = fmt.Fprintf(w, "%s:%d: `// guarantee: %s` names no exported identifier of its package\n", m.File, m.Line, m.Name)
	}
	total := len(r.Matched) + len(r.Unmatched)
	_, _ = fmt.Fprintf(w, "guarantee: %d exported identifiers state a guarantee; %d have a test marker, %d do not; %d stale marker(s)\n",
		total, len(r.Matched), len(r.Unmatched), len(r.Stale))
}

// pkgDir is one directory of Go files: what a marker of its tests can name.
type pkgDir struct {
	findings []Finding         // the exported identifiers that state a guarantee
	exported map[string]bool   // every exported identifier, to tell a stale marker
	markers  map[string]Marker // by name
	order    []Marker
}

// Scan reads every Go package under root and matches its guarantees with the markers of its tests.
func Scan(root string) (Result, error) {
	var res Result
	dirs := map[string]*pkgDir{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		return scanFile(fset, root, path, dirs)
	})
	if err != nil {
		return Result{}, fmt.Errorf("scanning %s: %w", root, err)
	}
	handOver(dirs)
	for _, dir := range sortedKeys(dirs) {
		dirs[dir].match(&res)
	}
	sort.SliceStable(res.Unmatched, func(i, j int) bool { return less(res.Unmatched[i], res.Unmatched[j]) })
	return res, nil
}

func less(a, b Finding) bool {
	if a.File != b.File {
		return a.File < b.File
	}
	return a.Line < b.Line
}

func sortedKeys(m map[string]*pkgDir) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// match sorts the findings of one directory into those a marker names and those it does not, and
// finds the markers that name nothing.
func (p *pkgDir) match(res *Result) {
	for _, f := range p.findings {
		if _, ok := p.markers[f.Name]; ok {
			res.Matched = append(res.Matched, f)
		} else {
			res.Unmatched = append(res.Unmatched, f)
		}
	}
	for _, m := range p.order {
		if !p.exported[m.Name] && !m.handed {
			res.Stale = append(res.Stale, m)
		}
	}
}

// scanFile parses one file and records its guarantees (a source file) or its markers (a test file)
// in the entry of its directory.
func scanFile(fset *token.FileSet, root, path string, dirs map[string]*pkgDir) error {
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	dir := filepath.ToSlash(filepath.Dir(rel))
	p := dirs[dir]
	if p == nil {
		p = &pkgDir{exported: map[string]bool{}, markers: map[string]Marker{}}
		dirs[dir] = p
	}
	if strings.HasSuffix(path, "_test.go") {
		p.addMarkers(fset, rel, f)
		return nil
	}
	p.addDecls(fset, rel, f)
	return nil
}

// addMarkers records the `// guarantee:` comments of a test file.
func (p *pkgDir) addMarkers(fset *token.FileSet, rel string, f *ast.File) {
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			m := markerLine.FindStringSubmatch(c.Text)
			if m == nil {
				continue
			}
			for _, name := range strings.FieldsFunc(m[1], func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
				mk := Marker{File: rel, Line: fset.Position(c.Pos()).Line, Name: strings.Trim(name, "`")}
				p.addMarker(mk)
				p.order = append(p.order, mk)
			}
		}
	}
}

// addMarker records a marker for its name, the first one of a name being the one kept.
func (p *pkgDir) addMarker(mk Marker) {
	if _, dup := p.markers[mk.Name]; !dup {
		p.markers[mk.Name] = mk
	}
}

// handOver gives a marker of the form `<dir>.<Name>` to the package in the directory whose last element
// is <dir>, when that package has an exported identifier <Name>: a promise a package states is often
// kept by the code of another (the write gate of model is run by the client of the root package), and
// the test that pins it is in that other package. A method or field name (`Client.Close`) is no address,
// as no directory is named like an exported type, and a directory name that two directories share is no
// address either.
func handOver(dirs map[string]*pkgDir) {
	byBase := map[string]string{} // last element of a directory -> the directory; "" when two share it
	for _, dir := range sortedKeys(dirs) {
		if dir == "." {
			continue
		}
		base := path.Base(dir)
		if _, dup := byBase[base]; dup {
			byBase[base] = ""
			continue
		}
		byBase[base] = dir
	}
	for _, dir := range sortedKeys(dirs) {
		p := dirs[dir]
		for i, mk := range p.order {
			target, ok := handTarget(dirs, byBase, dir, mk.Name)
			if !ok {
				continue
			}
			mk.Name = mk.Name[strings.Index(mk.Name, ".")+1:]
			dirs[target].addMarker(mk)
			p.order[i].handed = true
		}
	}
}

// handTarget finds the directory a marker name `<dir>.<Name>` hands the marker to, if it does.
func handTarget(dirs map[string]*pkgDir, byBase map[string]string, ownDir, name string) (string, bool) {
	base, rest, ok := strings.Cut(name, ".")
	if !ok {
		return "", false
	}
	target := byBase[base]
	if target == "" || target == ownDir || !dirs[target].exported[rest] {
		return "", false
	}
	return target, true
}

// sentenceOf returns the first sentence of the comment that states a guarantee, or "".
func sentenceOf(doc *ast.CommentGroup) string {
	if doc == nil {
		return ""
	}
	text := strings.Join(strings.Fields(doc.Text()), " ")
	for _, s := range splitSentences(text) {
		lower := notPromises.ReplaceAllString(strings.ToLower(s), "")
		for _, re := range promises {
			if re.MatchString(lower) {
				return clip(s)
			}
		}
	}
	return ""
}

// splitSentences cuts text after a full stop, question or exclamation mark followed by a space.
func splitSentences(text string) []string {
	var out []string
	start := 0
	for i := 0; i+1 < len(text); i++ {
		if (text[i] == '.' || text[i] == '?' || text[i] == '!') && text[i+1] == ' ' {
			out = append(out, text[start:i+1])
			start = i + 2
		}
	}
	return append(out, text[start:])
}

func clip(s string) string {
	if len(s) <= maxSentence {
		return s
	}
	return strings.TrimSpace(s[:maxSentence]) + "..."
}
