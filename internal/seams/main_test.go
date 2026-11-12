package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// gitIn runs git in dir with a fixed identity and no configuration of the machine behind it.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir, "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
	cmd.Env = append(withoutGitVariables(os.Environ()),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@example.test", "GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@example.test")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// commit writes files (an empty content removes the file) and commits them.
func commit(t *testing.T, dir, message string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if content == "" {
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", message)
}

const (
	libV1 = `package lib

// Open connects.
func Open() *Conn { return &Conn{} }

type Conn struct{}

// Read reads.
func (c *Conn) Read() int { return helper() }

func helper() int { return 1 }

func Stable() {}

func Gone() {}

const Limit = 5
`
	// The helper changed, and a comment: only the helper is a change.
	libV2 = `package lib

// Open connects.
func Open() *Conn { return &Conn{} }

type Conn struct{}

// Read reads, and says so.
func (c *Conn) Read() int { return helper() }

func helper() int { return 2 }

// Stable does nothing, as before.
func Stable() {}

func Gone() {}

const Limit = 5
`
	// Open takes an address, Write is new, Gone is removed, Limit is 6.
	libV3 = `package lib

// Open connects.
func Open(addr string) *Conn { return &Conn{} }

type Conn struct{}

// Read reads, and says so.
func (c *Conn) Read() int { return helper() }

func (c *Conn) Write() {}

func helper() int { return 2 }

// Stable does nothing, as before.
func Stable() {}

const Limit = 6
`
	subV1 = "package sub\n\nfunc Fn() {}\n"
	subV2 = "package sub\n\nfunc Fn() { _ = 1 }\n"
	goMod = "module example.test/lib\n\ngo 1.26\n"
)

// fixtureRepo makes a repository with three tagged versions and returns its directory. v0.1.0 is
// libV1, v0.2.0 is libV2 and subV2 (two commits), v0.3.0 is libV3 (one more commit).
func fixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	commit(t, dir, "v1", map[string]string{"go.mod": goMod, "lib.go": libV1, "sub/sub.go": subV1, "lib_test.go": "package lib\n"})
	gitIn(t, dir, "tag", "v0.1.0")
	commit(t, dir, "fix the helper", map[string]string{"lib.go": libV2})
	commit(t, dir, "change sub", map[string]string{"sub/sub.go": subV2, "README.md": "text"})
	gitIn(t, dir, "tag", "v0.2.0")
	commit(t, dir, "change the API", map[string]string{"lib.go": libV3})
	gitIn(t, dir, "tag", "v0.3.0")
	return dir
}

func runIn(t *testing.T, dir string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, func(string) string { return "" }, dir, &out, &errOut)
	return code, out.String(), errOut.String()
}

const consumerMain = `package main

import "example.test/lib"

func main() {
	c := lib.Open("x")
	c.Read()
}
`

// consumerDir is a checkout of a program that uses the fixture library.
func consumerDir(t *testing.T) string {
	t.Helper()
	return writeTree(t, map[string]string{"main.go": consumerMain})
}

func TestTheListOfAFixtureRange(t *testing.T) {
	dir := fixtureRepo(t)
	consumer := consumerDir(t)
	for _, tc := range []struct {
		name string
		args []string
		env  map[string]string
		want string
	}{
		{
			name: "without a consumer",
			args: []string{"v0.1.0", "v0.2.0"},
			want: `seams v0.1.0..v0.2.0: 2 commits, module example.test/lib
consumer: none (-provider DIR or SEAMS_PROVIDER): every changed exported symbol is listed

Exported symbols: 2, 1 changed themselves and 1 reach a changed declaration
  SYMBOL         CHANGE      FILE
  lib.Conn.Read  via helper  lib.go
  sub.Fn         body        sub/sub.go

Changed declarations that are not exported, with the exported symbols that reach them: 1
  DECLARATION  CHANGE  FILE    REACHED BY
  lib.helper   body    lib.go  1 exported symbol

Production files touched by more than one commit: 0
`,
		},
		{
			name: "a longer range: every kind of change, and a file two commits touched",
			args: []string{"v0.1.0", "v0.3.0"},
			want: `seams v0.1.0..v0.3.0: 3 commits, module example.test/lib
consumer: none (-provider DIR or SEAMS_PROVIDER): every changed exported symbol is listed

Exported symbols: 6, 5 changed themselves and 1 reach a changed declaration
  SYMBOL          CHANGE      FILE
  lib.Conn.Read   via helper  lib.go
  lib.Conn.Write  added       lib.go
  lib.Gone        removed     lib.go
  lib.Limit       value       lib.go
  lib.Open        signature   lib.go
  sub.Fn          body        sub/sub.go

Changed declarations that are not exported, with the exported symbols that reach them: 1
  DECLARATION  CHANGE  FILE    REACHED BY
  lib.helper   body    lib.go  1 exported symbol

Production files touched by more than one commit: 1
  COMMITS  FILE
  2        lib.go
`,
		},
		{
			name: "a branch name is a revision, one commit is not plural",
			args: []string{"v0.2.0", "main"},
			want: `seams v0.2.0..main: 1 commit, module example.test/lib
consumer: none (-provider DIR or SEAMS_PROVIDER): every changed exported symbol is listed

Exported symbols: 4, 4 changed themselves and 0 reach a changed declaration
  SYMBOL          CHANGE     FILE
  lib.Conn.Write  added      lib.go
  lib.Gone        removed    lib.go
  lib.Limit       value      lib.go
  lib.Open        signature  lib.go

Changed declarations that are not exported, with the exported symbols that reach them: 0

Production files touched by more than one commit: 0
`,
		},
		{
			name: "with a consumer, only the symbols it uses",
			args: []string{"-provider", consumer, "v0.1.0", "v0.3.0"},
			want: `seams v0.1.0..v0.3.0: 3 commits, module example.test/lib
consumer: Go files read: 1, importing the library: 1

Exported symbols: 2, 1 changed themselves and 1 reach a changed declaration (and 4 the consumer does not use; -all lists them)
  SYMBOL         CHANGE      FILE    USED BY THE CONSUMER
  lib.Conn.Read  via helper  lib.go  1: ~main.go:7
  lib.Open       signature   lib.go  1: main.go:6

Changed declarations that are not exported, with the exported symbols that reach them: 1
  DECLARATION  CHANGE  FILE    REACHED BY
  lib.helper   body    lib.go  1 exported symbol, 1 used by the consumer

Production files touched by more than one commit: 1
  COMMITS  FILE
  2        lib.go
`,
		},
		{
			name: "the consumer from the environment, and -all",
			args: []string{"-all", "v0.2.0", "v0.3.0"},
			env:  map[string]string{providerVariable: consumer},
			want: `seams v0.2.0..v0.3.0: 1 commit, module example.test/lib
consumer: Go files read: 1, importing the library: 1

Exported symbols: 4, 4 changed themselves and 0 reach a changed declaration
  SYMBOL          CHANGE     FILE    USED BY THE CONSUMER
  lib.Conn.Write  added      lib.go  -
  lib.Gone        removed    lib.go  -
  lib.Limit       value      lib.go  -
  lib.Open        signature  lib.go  1: main.go:6

Changed declarations that are not exported, with the exported symbols that reach them: 0

Production files touched by more than one commit: 0
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			getenv := func(k string) string { return tc.env[k] }
			code := run(tc.args, getenv, dir, &out, &errOut)
			if code != exitOK || errOut.Len() != 0 {
				t.Fatalf("exit %d, stderr %q", code, errOut.String())
			}
			if out.String() != tc.want {
				t.Errorf("output:\n%s\nwant:\n%s", out.String(), tc.want)
			}
		})
	}
}

// TestTheTableNamesAtMostThreeHelpersAndSites needs a symbol that reaches many changed helpers and is used in
// many places; -all lifts both limits.
func TestTheTableNamesAtMostThreeHelpersAndSites(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	const before = "package lib\nfunc F() { a(); b(); c(); d() }\nfunc G() { a(); b(); c() }\nfunc a() int { return 1 }\nfunc b() int { return 1 }\nfunc c() int { return 1 }\nfunc d() int { return 1 }\n"
	after := strings.ReplaceAll(before, "return 1", "return 2")
	commit(t, dir, "one", map[string]string{"go.mod": goMod, "lib.go": before})
	gitIn(t, dir, "tag", "v1.0.0")
	commit(t, dir, "two", map[string]string{"lib.go": after})
	use := writeTree(t, map[string]string{"main.go": "package main\nimport \"example.test/lib\"\nfunc main() {\n" + strings.Repeat("\tlib.F()\n", 4) + "\tlib.G()\n}\n"})

	_, out, _ := runIn(t, dir, "-provider", use, "v1.0.0", "HEAD")
	if !strings.Contains(out, "via a, b, c, ... (1 more)") || !hasLine(out, "lib.G", "via a, b, c ") || hasLine(out, "lib.G", "...") || !strings.Contains(out, "4: main.go:4, main.go:5, main.go:6, ...") {
		t.Errorf("limited output:\n%s", out)
	}
	_, out, _ = runIn(t, dir, "-all", "-provider", use, "v1.0.0", "HEAD")
	if !strings.Contains(out, "via a, b, c, d ") || !strings.Contains(out, "4: main.go:4, main.go:5, main.go:6, main.go:7") {
		t.Errorf("-all output:\n%s", out)
	}
}

// hasLine reports whether a line of out starts (after the indent) with first and contains part.
func hasLine(out, first, part string) bool {
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), first+" ") && strings.Contains(line, part) {
			return true
		}
	}
	return false
}

func TestTheCommandRefusesAWrongRange(t *testing.T) {
	dir := fixtureRepo(t)
	gitIn(t, dir, "checkout", "-q", "-b", "side", "v0.1.0")
	commit(t, dir, "sideways", map[string]string{"sub/sub.go": "package sub\n\nfunc Other() {}\n"})
	gitIn(t, dir, "checkout", "-q", "main")
	dirty := fixtureRepo(t)
	if err := os.WriteFile(filepath.Join(dirty, "lib.go"), []byte("package lib\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		repo    string
		args    []string
		wantErr string
	}{
		{"an unknown tag", dir, []string{"v0.1.0", "v9.9.9"}, `unknown revision "v9.9.9"`},
		{"an unknown first revision", dir, []string{"v9.9.9", "v0.1.0"}, `unknown revision "v9.9.9"`},
		{"the same commit under two names", dir, []string{"v0.3.0", "main"}, "the range is empty"},
		{"a range that runs backward", dir, []string{"v0.2.0", "v0.1.0"}, "v0.2.0 is not an ancestor of v0.1.0"},
		{"a revision on another branch", dir, []string{"v0.2.0", "side"}, "v0.2.0 is not an ancestor of side"},
		{"a range that ends at a dirty checkout", dirty, []string{"v0.1.0", "HEAD"}, "uncommitted changes"},
		{"a directory that is no repository", t.TempDir(), []string{"v0.1.0", "v0.2.0"}, `unknown revision "v0.1.0"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runIn(t, tc.repo, tc.args...)
			if code != exitFailure || out != "" || !strings.HasPrefix(errOut, "seams: ") || !strings.Contains(errOut, tc.wantErr) {
				t.Errorf("exit %d, stdout %q, stderr %q; want exit 1 and %q", code, out, errOut, tc.wantErr)
			}
		})
	}
	t.Run("a dirty checkout is fine when the range ends before it", func(t *testing.T) {
		if code, _, errOut := runIn(t, dirty, "v0.1.0", "v0.2.0"); code != exitOK {
			t.Errorf("exit %d: %s", code, errOut)
		}
	})
}

func TestTheCommandRefusesAWrongCommandLine(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"no arguments", nil},
		{"one revision", []string{"v0.1.0"}},
		{"three revisions", []string{"a", "b", "c"}},
		{"an unknown flag", []string{"-nope", "a", "b"}},
		{"a revision that looks like a flag", []string{"--", "-x", "b"}},
		{"an empty revision", []string{"a", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runIn(t, t.TempDir(), tc.args...)
			if code != exitUsage || out != "" || !strings.Contains(errOut, usageLine) {
				t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
			}
		})
	}
}

func TestTheCommandRefusesABadConsumerOrARevisionItCannotRead(t *testing.T) {
	dir := fixtureRepo(t)
	noModule := t.TempDir()
	gitIn(t, noModule, "init", "-q", "-b", "main")
	commit(t, noModule, "one", map[string]string{"a.go": "package a\n"})
	gitIn(t, noModule, "tag", "t1")
	commit(t, noModule, "two", map[string]string{"a.go": "package a\n// x\n"})
	badSyntax := t.TempDir()
	gitIn(t, badSyntax, "init", "-q", "-b", "main")
	commit(t, badSyntax, "one", map[string]string{"go.mod": goMod, "a.go": "package a\n"})
	gitIn(t, badSyntax, "tag", "t1")
	commit(t, badSyntax, "two", map[string]string{"a.go": "package a\nfunc ("})
	noModuleLine := t.TempDir()
	gitIn(t, noModuleLine, "init", "-q", "-b", "main")
	commit(t, noModuleLine, "one", map[string]string{"go.mod": "go 1.26\n", "a.go": "package a\n"})
	gitIn(t, noModuleLine, "tag", "t1")
	commit(t, noModuleLine, "two", map[string]string{"a.go": "package a\n// x\n"})
	for _, tc := range []struct {
		name    string
		repo    string
		args    []string
		wantErr string
	}{
		{"a consumer that does not exist", dir, []string{"-provider", filepath.Join(t.TempDir(), "nope"), "v0.1.0", "v0.2.0"}, "read "},
		{"a consumer that is not one", dir, []string{"-provider", writeTree(t, map[string]string{"a.go": "package a\n"}), "v0.1.0", "v0.2.0"}, "imports example.test/lib"},
		{"a repository that is no Go module", noModule, []string{"t1", "HEAD"}, "t1: no go.mod"},
		{"a go.mod without a module line", noModuleLine, []string{"t1", "HEAD"}, "t1: go.mod has no module line"},
		{"a file that does not parse", badSyntax, []string{"t1", "HEAD"}, "HEAD: parse a.go"},
		{"a file that does not parse in the older revision", badSyntax, []string{"HEAD", "HEAD"}, "empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, _, errOut := runIn(t, tc.repo, tc.args...)
			if code != exitFailure || !strings.Contains(errOut, tc.wantErr) {
				t.Errorf("exit %d, stderr %q; want exit 1 and %q", code, errOut, tc.wantErr)
			}
		})
	}
}

// intercepting wraps git: a command for which fake returns handled=true gets its answer.
func intercepting(g gitFunc, fake func(args []string) (out string, err error, handled bool)) gitFunc {
	return func(args ...string) (string, error) {
		if out, err, handled := fake(args); handled {
			return out, err
		}
		return g(args...)
	}
}

func TestExecuteReportsAGitFailureAtEachStep(t *testing.T) {
	dir := fixtureRepo(t)
	boom := errors.New("boom")
	for _, tc := range []struct {
		name string
		hit  func(args []string) bool
		out  string
		want string
	}{
		{"HEAD cannot be read", func(a []string) bool { return a[0] == "rev-parse" && a[1] == "HEAD" }, "", "boom"},
		{"the status cannot be read", func(a []string) bool { return a[0] == "status" }, "", "boom"},
		{"the commits cannot be counted", func(a []string) bool { return a[0] == "rev-list" }, "", "boom"},
		{"the count is not a number", func(a []string) bool { return a[0] == "rev-list" }, "many", "git rev-list --count"},
		{"the files cannot be listed", func(a []string) bool { return a[0] == "ls-tree" }, "", "boom"},
		{"a file cannot be read", func(a []string) bool { return a[0] == "show" && strings.HasSuffix(a[1], ":lib.go") }, "", "boom"},
		{"the log cannot be read", func(a []string) bool { return a[0] == "log" }, "", "boom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := intercepting(systemGit(dir), func(a []string) (string, error, bool) {
				if !tc.hit(a) {
					return "", nil, false
				}
				if tc.out != "" {
					return tc.out, nil, true
				}
				return "", boom, true
			})
			err := execute(options{from: "v0.1.0", to: "HEAD"}, g, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

type failingWriter struct{ after int }

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.after <= 0 {
		return 0, errors.New("disk full")
	}
	w.after--
	return len(p), nil
}

func TestExecuteReportsAWriteFailureAtEachPart(t *testing.T) {
	dir := fixtureRepo(t)
	for after := range 6 {
		t.Run(fmt.Sprint("after ", after, " writes"), func(t *testing.T) {
			err := execute(options{from: "v0.1.0", to: "v0.3.0"}, systemGit(dir), &failingWriter{after: after})
			if err == nil || !strings.Contains(err.Error(), "disk full") {
				t.Errorf("err = %v", err)
			}
		})
	}
}

func TestWithoutGitVariables(t *testing.T) {
	got := withoutGitVariables([]string{"PATH=/bin", "GIT_DIR=/x/.git", "HOME=/h", "GIT_INDEX_FILE=/x/index", "GITHUB=1", "NOT_GIT_DIR=2"})
	want := []string{"PATH=/bin", "HOME=/h", "GITHUB=1", "NOT_GIT_DIR=2"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// A pre-commit hook runs the tests with GIT_DIR and GIT_INDEX_FILE of the repository being committed
// in the environment; the command must still read the directory it is given, and must leave that
// repository alone.
func TestTheCommandReadsTheDirectoryItIsGivenWhateverGitDirSays(t *testing.T) {
	dir := fixtureRepo(t)
	other := t.TempDir()
	gitIn(t, other, "init", "-q", "-b", "main")
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_INDEX_FILE", filepath.Join(other, ".git", "index"))
	code, out, errOut := runIn(t, dir, "v0.1.0", "v0.2.0")
	if code != exitOK || !strings.Contains(out, "lib.Conn.Read") {
		t.Errorf("exit %d, stderr %q, stdout %q", code, errOut, out)
	}
	if refs := gitIn(t, other, "for-each-ref"); refs != "" {
		t.Errorf("the other repository was written to: %s", refs)
	}
}

func TestCommitCountsKeepsFilesOfMoreThanOneCommitMostTouchedFirst(t *testing.T) {
	log := "\x00\nc.go\nb.go\na.go\nREADME.md\nb_test.go\n\x00\nb.go\na.go\nb.go\nc.go\n\x00\nc.go\nonly.go\n"
	g := func(args ...string) (string, error) { return log, nil }
	got, err := commitCounts(g, revRange{fromSHA: "a", toSHA: "b"})
	if err != nil {
		t.Fatal(err)
	}
	want := []fileCount{{"c.go", 3}, {"a.go", 2}, {"b.go", 2}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want { // subtests: one expected order, compared element by element
		if got[i] != want[i] {
			t.Errorf("got %v, want %v (a file named twice in one commit counts once; ties by name)", got, want)
			break
		}
	}
}

func TestRenderWithNothingToListPrintsTheHeadingsOnly(t *testing.T) {
	var out bytes.Buffer
	err := render(&out, report{opts: options{from: "a", to: "b"}, rng: revRange{commits: 2}, module: "m"})
	if err != nil {
		t.Fatal(err)
	}
	const want = "seams a..b: 2 commits, module m\n" +
		"consumer: none (-provider DIR or SEAMS_PROVIDER): every changed exported symbol is listed\n\n" +
		"Exported symbols: 0, 0 changed themselves and 0 reach a changed declaration\n\n" +
		"Changed declarations that are not exported, with the exported symbols that reach them: 0\n\n" +
		"Production files touched by more than one commit: 0\n"
	if out.String() != want {
		t.Errorf("output:\n%s", out.String())
	}
}
