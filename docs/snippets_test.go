package docs

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The Go code blocks of the README and of the guides are the first thing a user pastes, so each one
// has to compile against the code of this checkout. This test extracts every ```go fence of README.md
// and docs/*.md, wraps it in a package of a temporary module that replaces the library with the
// checkout, and vets the lot. A block that is only a piece of a program says so with `// fragment` as
// its first line and is left out. It runs offline: the module cache already holds what the library
// itself needs.

// fragmentMarker starts the first line of a block that is not meant to compile on its own (a
// reason may follow it).
const fragmentMarker = "// fragment"

// fence is one ```go block: where its first code line is, and the code.
type fence struct {
	file string // slash-separated path from the repository root
	line int    // the line of the first code line
	code string
}

// fences returns the ```go blocks of a Markdown text, in order.
func fences(file, text string) []fence {
	var out []fence
	var cur *fence
	var body []string
	sc := bufio.NewScanner(strings.NewReader(text))
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		switch {
		case cur == nil && strings.TrimSpace(line) == "```go":
			cur = &fence{file: file, line: n + 1}
			body = nil
		case cur != nil && strings.TrimSpace(line) == "```":
			cur.code = strings.Join(body, "\n") + "\n"
			out = append(out, *cur)
			cur = nil
		case cur != nil:
			body = append(body, line)
		}
	}
	return out
}

// snippetHeader gives a block everything the prose around it takes for granted: the packages the
// guides use (each referenced, so that an import the block does not need is no error) and the
// variables they leave out.
const snippetHeader = `package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/davidlukac/escp-go"
	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/model"
	"github.com/davidlukac/escp-go/safety"
)

var (
	_ = errors.New
	_ = fmt.Sprint
	_ = slog.New
	_ = os.Exit
	_ = testing.Short
	_ = time.Second
	_ = escp.Dial
	_ = escptest.Start
	_ = model.ParseMAC
	_ = safety.Chain
)

// What the prose leaves out.
var (
	ctx                context.Context
	pw, host, user, id string
)

func main() {}
`

var (
	importLine = regexp.MustCompile(`^(\s*)("[^"]+")`)
	topLevel   = regexp.MustCompile(`^(func|type|var|const)\s`)
)

// source turns a block into a Go file, or reports that it is a fragment. An import list is checked
// by importing each path under the blank name (a path that does not exist fails); a block that starts
// with a declaration is a file of its own; anything else is a sequence of statements, wrapped in a
// function.
func (f fence) source() (src string, fragment bool) {
	first, _, _ := strings.Cut(f.code, "\n")
	directive := fmt.Sprintf("//line %s:%d\n", f.file, f.line)
	switch {
	case strings.HasPrefix(strings.TrimSpace(first), fragmentMarker):
		return "", true
	case strings.HasPrefix(first, "import"):
		var b strings.Builder
		b.WriteString("package main\n\n" + directive)
		for line := range strings.Lines(f.code) {
			b.WriteString(importLine.ReplaceAllString(line, "${1}_ ${2}"))
		}
		return b.String() + "\nfunc main() {}\n", false
	case topLevel.MatchString(first):
		return snippetHeader + "\n" + directive + f.code, false
	default:
		return snippetHeader + "\nfunc snippet() {\n" + directive + f.code + "}\n", false
	}
}

// markdownFiles lists README.md and the Markdown files of docs/, relative to the repository root.
func markdownFiles(t *testing.T) []string {
	t.Helper()
	guides, err := filepath.Glob(filepath.Join("..", "docs", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	files := []string{filepath.Join("..", "README.md")}
	files = append(files, guides...)
	slices.Sort(files)
	return files
}

// snippetModule writes the go.mod of the temporary module: the library's own requirements, with the
// library itself replaced by the checkout.
func snippetModule(t *testing.T, dir, root string) {
	t.Helper()
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	sum, err := os.ReadFile(filepath.Join(root, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	_, rest, _ := strings.Cut(string(mod), "\n") // everything after the module line
	gomod := "module snippets\n" + rest +
		"\nrequire github.com/davidlukac/escp-go v0.0.0\n" +
		"replace github.com/davidlukac/escp-go => " + strconv.Quote(root) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestEveryGoBlockOfTheDocumentationCompiles(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	snippetModule(t, dir, root)

	checked := 0
	for _, file := range markdownFiles(t) {
		text, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		rel := filepath.ToSlash(strings.TrimPrefix(file, ".."+string(filepath.Separator)))
		for i, f := range fences(rel, string(text)) {
			src, fragment := f.source()
			if fragment {
				continue
			}
			pkg := filepath.Join(dir, fmt.Sprintf("b%02d_%s_%d", i, strings.NewReplacer("/", "_", ".", "_").Replace(rel), f.line))
			if err := os.MkdirAll(pkg, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(pkg, "main.go"), []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			checked++
		}
	}
	if checked < 8 {
		t.Fatalf("found only %d Go blocks in the README and the guides: the scan is broken", checked)
	}

	goTool, err := exec.LookPath("go") // the command that runs this test is on its PATH
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, goTool, "vet", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GOTOOLCHAIN=local", "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("a Go block of the documentation does not compile (file:line are those of the Markdown; "+
			"mark a block that is only a fragment with a first line `%s`): %v\n%s", fragmentMarker, err, out)
	}
}

func TestFencesFindsEveryGoBlockAndNothingElse(t *testing.T) {
	text := "# T\n\n```sh\ngo get x\n```\n\n```go\na := 1\n```\n\ntext\n\n```go\n// fragment\nb()\n```\n"
	got := fences("x.md", text)
	if len(got) != 2 {
		t.Fatalf("found %d blocks, want 2: %+v", len(got), got)
	}
	if got[0].line != 8 || got[0].code != "a := 1\n" || got[1].line != 14 {
		t.Errorf("blocks = %+v", got)
	}
}

func TestSourceTreatsEachKindOfBlockAsItsKind(t *testing.T) {
	tests := []struct {
		name     string
		code     string
		fragment bool
		contains []string
	}{
		{name: "fragment", code: "// fragment\nx()\n", fragment: true},
		{name: "statements", code: "a := 1\n", contains: []string{"func snippet() {\n//line x.md:3\na := 1\n}"}},
		{name: "declaration", code: "func TestX(t *testing.T) {}\n", contains: []string{"//line x.md:3\nfunc TestX"}},
		{name: "imports", code: "import (\n\t\"fmt\" // the printer\n)\n", contains: []string{"\t_ \"fmt\" // the printer"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src, fragment := fence{file: "x.md", line: 3, code: tc.code}.source()
			if fragment != tc.fragment {
				t.Fatalf("fragment = %v, want %v", fragment, tc.fragment)
			}
			for _, want := range tc.contains {
				if !strings.Contains(src, want) {
					t.Errorf("source lacks %q:\n%s", want, src)
				}
			}
		})
	}
}
