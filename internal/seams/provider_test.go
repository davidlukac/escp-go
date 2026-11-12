package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree writes files (relative path -> content) under a new temporary directory.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// libraryTree is a library of two packages: the root, "lib", and "sub".
func libraryTree(t *testing.T) *tree {
	t.Helper()
	return treeOf(t, map[string]string{
		"lib.go":     "package lib\nfunc Open() {}\ntype Conn struct{}\nfunc (c *Conn) Read() {}\n",
		"sub/sub.go": "package sub\nfunc Fn() {}\n",
	})
}

const consumerSource = `package main

import (
	"fmt"
	"example.test/lib"
	renamed "example.test/lib/sub"
	_ "example.test/lib/blank"
	other "example.test/other"
)

func main() {
	c := lib.Open()
	c.Read()
	renamed.Fn()
	other.Open()
	fmt.Println()
}
`

func TestScanConsumerFindsTheUsesOfTheLibrary(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"main.go":                     consumerSource,
		"main_test.go":                "package main\nimport \"example.test/lib\"\nfunc f() { lib.Open() }\n",
		"vendor/x/x.go":               "package x\nimport \"example.test/lib\"\nfunc f() { lib.Open() }\n",
		".hidden/x.go":                "package x\nimport \"example.test/lib\"\nfunc f() { lib.Open() }\n",
		"testdata/x.go":               "package x\nimport \"example.test/lib\"\nfunc f() { lib.Open() }\n",
		"notes.txt":                   "lib.Open()",
		"internal/provider/p.go":      "package provider\nimport \"example.test/lib\"\nfunc f() { _ = lib.Open }\n",
		"internal/provider/plain.go":  "package provider\nfunc g() {}\n",
		"internal/provider/plain2.go": "package provider\nfunc h() {}\n",
	})
	u, err := scanConsumer(dir, libraryTree(t))
	if err != nil {
		t.Fatal(err)
	}
	if u.files != 5 || u.library != 3 {
		t.Errorf("files = %d, library = %d; want 5 and 3 (vendor, testdata, hidden and non-Go files are skipped)", u.files, u.library)
	}
	open := describeSites(u.exact[nodeKey(".", "", "Open")], 0)
	if open != "3: internal/provider/p.go:3, main.go:12, main_test.go:3" {
		t.Errorf("lib.Open sites = %q", open)
	}
	if got := describeSites(u.exact[nodeKey("sub", "", "Fn")], 0); got != "1: main.go:14" {
		t.Errorf("sub.Fn sites (through an alias) = %q", got)
	}
	if got := describeSites(u.exact[nodeKey("lib/blank", "", "X")], 0); got != "-" {
		t.Errorf("a blank import names nothing: %q", got)
	}
	read := &node{recv: "Conn", name: "Read"}
	if got := describeSites(u.sitesOf(read), 0); got != "1: ~main.go:13" {
		t.Errorf("Conn.Read by name = %q", got)
	}
	if got := u.sitesOf(&node{dir: ".", name: "Open"}); len(got) != 3 {
		t.Errorf("sitesOf a function = %v", got)
	}
}

func TestScanConsumerRefusals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		dir   func(t *testing.T) string
		wants string
	}{
		{"a missing directory", func(t *testing.T) string {
			t.Helper()
			return filepath.Join(t.TempDir(), "nope")
		}, "read "},
		{"no Go file", func(t *testing.T) string {
			t.Helper()
			return writeTree(t, map[string]string{"a.txt": "x"})
		}, "no Go file under"},
		{"no import of the library", func(t *testing.T) string {
			t.Helper()
			return writeTree(t, map[string]string{"a.go": "package a\nimport \"fmt\"\nfunc f() { fmt.Println() }\n"})
		}, "imports example.test/lib"},
		{"a syntax error", func(t *testing.T) string {
			t.Helper()
			return writeTree(t, map[string]string{"a.go": "package a\nfunc ("})
		}, "a.go"},
		{"a file that cannot be read (a link to nothing)", func(t *testing.T) string {
			t.Helper()
			dir := t.TempDir()
			if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "a.go")); err != nil {
				t.Skipf("no symbolic links here: %v", err)
			}
			return dir
		}, "read "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := scanConsumer(tc.dir(t), libraryTree(t))
			if err == nil || !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("err = %v, want it to contain %q", err, tc.wants)
			}
		})
	}
}

func TestLibraryDir(t *testing.T) {
	for _, tc := range []struct {
		in   string
		dir  string
		isIt bool
	}{
		{"example.test/lib", ".", true},
		{"example.test/lib/model", "model", true},
		{"example.test/library", "", false},
		{"fmt", "", false},
	} {
		t.Run(tc.in, func(t *testing.T) {
			dir, ok := libraryDir(tc.in, "example.test/lib")
			if dir != tc.dir || ok != tc.isIt {
				t.Errorf("libraryDir(%q) = %q, %v", tc.in, dir, ok)
			}
		})
	}
}

func TestDescribeSites(t *testing.T) {
	sites := []site{
		{file: "b_test.go", line: 1},
		{file: "b.go", line: 9},
		{file: "a.go", line: 2},
		{file: "b.go", line: 3, byName: true},
	}
	for _, tc := range []struct {
		name  string
		sites []site
		limit int
		want  string
	}{
		{"none", nil, 3, "-"},
		{"all, tests last", sites, 0, "4: a.go:2, ~b.go:3, b.go:9, b_test.go:1"},
		{"limited", sites, 2, "4: a.go:2, ~b.go:3, ..."},
		{"under the limit", sites[:1], 3, "1: b_test.go:1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := describeSites(tc.sites, tc.limit); got != tc.want {
				t.Errorf("describeSites = %q, want %q", got, tc.want)
			}
		})
	}
	if sites[0].file != "b_test.go" {
		t.Error("describeSites reordered the caller's slice")
	}
}
