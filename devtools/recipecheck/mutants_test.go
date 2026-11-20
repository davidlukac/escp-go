//go:build mutants

package main

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// installShim builds the report tool once and puts the stand-in `go` first on the PATH.
func installShim(t *testing.T, realRoot string) {
	t.Helper()
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	tool := filepath.Join(dir, "mutationreport")
	build := exec.Command(realGo, "build", "-o", tool, "./internal/mutationreport")
	build.Dir = realRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the report tool: %v\n%s", err, out)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o750); err != nil {
		t.Fatal(err)
	}
	dst, err := os.OpenFile(filepath.Join(bin, "go"), os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	if err := dst.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv(shimVariable, "1")
	t.Setenv("RECIPECHECK_REAL_GO", realGo)
	t.Setenv("RECIPECHECK_REPORT_TOOL", tool)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// mutant is a change to the Makefile that the check must notice: old is replaced by new at its
// occurrence-th place (counted from 0). A mutant with a reason in equivalent changes nothing the check
// can observe, and the check must not report it: that is how the list says why it is let through.
type mutant struct {
	Name string `json:"name"`
	// File is the file the mutant changes, relative to the repository: the Makefile when empty.
	File       string `json:"file"`
	Old        string `json:"old"`
	New        string `json:"new"`
	Occurrence int    `json:"occurrence"`
	Equivalent string `json:"equivalent"`
}

// apply replaces the occurrence-th place of Old in text; it fails the test when there is none, so that a
// mutant whose text no longer matches the Makefile cannot pass by changing nothing.
func (m mutant) apply(t *testing.T, text string) string {
	t.Helper()
	if m.Old == "" || m.Old == m.New {
		t.Fatalf("%s: the mutant changes nothing", m.Name)
	}
	at := -1
	for i := 0; i <= m.Occurrence; i++ {
		next := strings.Index(text[at+1:], m.Old)
		if next < 0 {
			t.Fatalf("%s: the Makefile has no occurrence %d of %q", m.Name, m.Occurrence, m.Old)
		}
		at += next + 1
	}
	return text[:at] + m.New + text[at+len(m.Old):]
}

// check applies the mutant to its file (the Makefile, or a workflow) and runs the check over the result.
func (m mutant) check(t *testing.T, realRoot, makefile string) []string {
	t.Helper()
	if m.File == "" {
		return checkCopy(t, realRoot, m.apply(t, makefile), nil)
	}
	if !slices.Contains(zizmorWorkflows, m.File) {
		t.Fatalf("%s: the mutant changes %s, which the check does not read", m.Name, m.File)
	}
	data, err := os.ReadFile(filepath.Join(realRoot, filepath.FromSlash(m.File)))
	if err != nil {
		t.Fatal(err)
	}
	return checkCopy(t, realRoot, makefile, map[string]string{m.File: m.apply(t, string(data))})
}

// copyRepository builds a scratch repository the recipes can run in: the Makefile (given), the workflows the
// check reads (those of the real repository, or the texts given in workflows by name), go.mod and
// go.sum, the sources of the report tool the recipes `go run`, and an empty Go file in every other
// directory that has Go files in the real repository (the Makefile lists them for its PKG filter).
func copyRepository(t *testing.T, realRoot, makefile string, workflows map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	put := func(name string, data []byte) {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	put("Makefile", []byte(makefile))
	for _, name := range zizmorWorkflows {
		text, ok := workflows[name]
		if !ok {
			data, err := os.ReadFile(filepath.Join(realRoot, filepath.FromSlash(name)))
			if err != nil {
				t.Fatal(err)
			}
			text = string(data)
		}
		put(filepath.FromSlash(name), []byte(text))
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join(realRoot, name))
		if err != nil {
			t.Fatal(err)
		}
		put(name, data)
	}
	err := filepath.WalkDir(realRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(realRoot, path)
		if d.IsDir() {
			switch d.Name() {
			case "build", ".git", "devtools", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") {
			return nil
		}
		data := []byte("package empty\n")
		if filepath.Dir(rel) == filepath.Join("internal", "mutationreport") && !strings.HasSuffix(rel, "_test.go") {
			var readErr error
			if data, readErr = os.ReadFile(path); readErr != nil {
				return readErr
			}
		}
		if filepath.Dir(rel) == filepath.Join("internal", "mutationreport") && strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		put(rel, data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// checkCopy runs the check over a scratch repository with the Makefile text and the workflow texts given, and returns
// its problems.
func checkCopy(t *testing.T, realRoot, makefile string, workflows map[string]string) []string {
	t.Helper()
	dir := copyRepository(t, realRoot, makefile, workflows)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c := &checker{root: dir, run: execRunner(dir, "make"), files: osFiles{}, standin: shellQuote(exe) + " " + standinCommand}
	problems, err := c.check(context.Background())
	if err != nil {
		t.Fatalf("the check could not run: %v", err)
	}
	return problems
}

// TestMakefileMutants holds the check to the list of mutants of the Makefile in
// testdata/makefile_mutants.json: the Makefile as it is passes, every mutant is reported except the ones
// the list gives a reason for, and those pass. It runs real make and `go run`, so it is built with the
// `mutants` tag (`make recipemutants`); the mutants run in parallel.
func TestMakefileMutants(t *testing.T) {
	realRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	makefile, err := os.ReadFile(filepath.Join(realRoot, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("testdata", "makefile_mutants.json"))
	if err != nil {
		t.Fatal(err)
	}
	installShim(t, realRoot)
	var mutants []mutant
	if err := json.Unmarshal(data, &mutants); err != nil {
		t.Fatal(err)
	}
	t.Run("the Makefile as it is", func(t *testing.T) {
		t.Parallel()
		if problems := checkCopy(t, realRoot, string(makefile), nil); len(problems) != 0 {
			t.Errorf("problems in the Makefile as it is:\n%s", strings.Join(problems, "\n"))
		}
	})
	names := map[string]bool{}
	t.Run("the mutants", func(t *testing.T) {
		for _, m := range mutants {
			if names[m.Name] {
				t.Fatalf("two mutants are called %q", m.Name)
			}
			names[m.Name] = true
			t.Run(m.Name, func(t *testing.T) {
				t.Parallel()
				problems := m.check(t, realRoot, string(makefile))
				switch {
				case m.Equivalent == "" && len(problems) == 0:
					t.Errorf("the check does not notice this mutant")
				case m.Equivalent != "" && len(problems) != 0:
					t.Errorf("a mutant listed as equivalent (%s) is reported:\n%s", m.Equivalent, strings.Join(problems, "\n"))
				}
			})
		}
	})
}
