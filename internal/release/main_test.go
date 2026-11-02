package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const changelogWithTwoVersions = `# Changelog

Introduction.

## 0.2.0

- two

## 0.1.0

- one
`

func TestParseVersion(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want version
		bad  bool
	}{
		{"0.4.0", version{0, 4, 0}, false},
		{"1.20.300", version{1, 20, 300}, false},
		{"v0.4.0", version{}, true},
		{"0.4", version{}, true},
		{"0.4.0.1", version{}, true},
		{"0.4.0-rc1", version{}, true},
		{"01.2.3", version{}, true},
		{"1.2.x", version{}, true},
		{"", version{}, true},
		{"99999999999999999999.0.0", version{}, true}, // does not fit an int
	} {
		got, err := parseVersion(tc.in)
		if (err != nil) != tc.bad || got != tc.want {
			t.Errorf("parseVersion(%q) = %v, %v; want %v, bad=%v", tc.in, got, err, tc.want, tc.bad)
		}
	}
	if got := (version{1, 2, 3}).String(); got != "1.2.3" {
		t.Errorf("String = %q", got)
	}
}

func TestNewerThan(t *testing.T) {
	for _, tc := range []struct {
		a, b version
		want bool
	}{
		{version{1, 0, 0}, version{0, 9, 9}, true},
		{version{0, 9, 9}, version{1, 0, 0}, false},
		{version{0, 5, 0}, version{0, 4, 9}, true},
		{version{0, 4, 9}, version{0, 5, 0}, false},
		{version{0, 4, 1}, version{0, 4, 0}, true},
		{version{0, 4, 0}, version{0, 4, 1}, false},
		{version{0, 4, 0}, version{0, 4, 0}, false},
	} {
		if got := tc.a.newerThan(tc.b); got != tc.want {
			t.Errorf("%v newerThan %v = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestInsertEntry(t *testing.T) {
	got, err := insertEntry(changelogWithTwoVersions, "0.3.0", "three")
	if err != nil {
		t.Fatal(err)
	}
	want := "# Changelog\n\nIntroduction.\n\n## 0.3.0\n\n- three\n\n## 0.2.0\n\n- two\n\n## 0.1.0\n\n- one\n"
	if got != want {
		t.Errorf("entry above the newest one:\n%q\nwant\n%q", got, want)
	}
	got, err = insertEntry("# Changelog\n\nIntroduction.\n\n\n", "0.1.0", "first")
	if err != nil || got != "# Changelog\n\nIntroduction.\n\n## 0.1.0\n\n- first\n" {
		t.Errorf("first entry = %q, %v", got, err)
	}
	// the newest entry may be the very first thing in the file
	got, err = insertEntry("\n## 0.1.0\n\n- one\n", "0.2.0", "two")
	if err != nil || got != "\n## 0.2.0\n\n- two\n\n## 0.1.0\n\n- one\n" {
		t.Errorf("entry above an entry at the top = %q, %v", got, err)
	}
	if _, err := insertEntry(changelogWithTwoVersions, "0.2.0", "again"); err == nil || !strings.Contains(err.Error(), "already has an entry '## 0.2.0'") {
		t.Errorf("a version with an entry: %v", err)
	}
	// "## 0.2.0" must be a heading of its own: a longer version or a mention in a sentence is not it
	if _, err := insertEntry("## 0.2.0.1\n\n- x about ## 0.2.0\n", "0.2.0", "y"); err != nil {
		t.Errorf("a different heading was taken for the entry: %v", err)
	}
}

// repo is a stand-in for git and make: it answers like a repository in the state the test describes
// and records what was asked.
type repo struct {
	branch      string
	status      string // git status --porcelain before anything is staged
	staged      string // ... after git add (empty: just CHANGELOG.md)
	tags        string
	files       map[string]string
	env         map[string]string
	fail        map[string]error // git subcommand -> failure
	lateFail    map[string]error // git subcommand -> failure once something is staged
	makeErr     error
	calls       []string
	added       bool
	unreadable  bool
	unwritable  bool
	madeTargets []string
}

func newRepo() *repo {
	return &repo{branch: "feature", tags: "v0.2.0\nv0.1.0", files: map[string]string{changelogFile: changelogWithTwoVersions},
		env: map[string]string{}, fail: map[string]error{}, lateFail: map[string]error{}}
}

func (r *repo) tools(out *bytes.Buffer) tools {
	return tools{
		git: func(args ...string) (string, error) {
			r.calls = append(r.calls, "git "+strings.Join(args, " "))
			if err := r.fail[args[0]]; err != nil {
				return "git said so", err
			}
			if err := r.lateFail[args[0]]; err != nil && r.added {
				return "git said so", err
			}
			switch args[0] {
			case "rev-parse":
				return r.branch, nil
			case "status":
				if r.added {
					if r.staged != "" {
						return r.staged, nil
					}
					return "M  " + changelogFile, nil
				}
				return r.status, nil
			case "tag":
				if args[1] == "--list" {
					return r.tags, nil
				}
			case "add":
				r.added = true
			}
			return "", nil
		},
		make: func(targets ...string) error {
			r.madeTargets = targets
			return r.makeErr
		},
		readFile: func(name string) ([]byte, error) {
			if r.unreadable {
				return nil, errors.New("cannot read")
			}
			return []byte(r.files[name]), nil
		},
		writeFile: func(name string, data []byte) error {
			if r.unwritable {
				return errors.New("cannot write")
			}
			r.files[name] = string(data)
			return nil
		},
		getenv: func(k string) string { return r.env[k] },
		stdout: out,
	}
}

func (r *repo) run(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(args, r.tools(&out), &errOut)
	return code, out.String(), errOut.String()
}

func TestPrepareAddsTheEntryRunsTheChecksAndCommitsTheChangelogAlone(t *testing.T) {
	r := newRepo()
	code, out, errOut := r.run("0.3.0", "  what changed  ")
	if code != exitOK || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	if !strings.Contains(r.files[changelogFile], "## 0.3.0\n\n- what changed\n\n## 0.2.0") {
		t.Errorf("the entry is missing or misplaced:\n%s", r.files[changelogFile])
	}
	if got := strings.Join(r.madeTargets, " "); got != "cover lint vuln" {
		t.Errorf("checks = %q", got)
	}
	if !strings.Contains(strings.Join(r.calls, "\n"), "git add CHANGELOG.md\n") || !strings.Contains(strings.Join(r.calls, "\n"), "git commit -q -m chore: release 0.3.0") {
		t.Errorf("calls:\n%s", strings.Join(r.calls, "\n"))
	}
	if strings.Contains(strings.Join(r.calls, "\n"), "-m chore: release 0.3.0 -m") {
		t.Error("a trailer was added though the environment names none")
	}
	if want := "committed the 0.3.0 entry on feature; push the branch, open a PR, squash merge, then: scripts/release.sh --tag 0.3.0\n"; out != want {
		t.Errorf("stdout = %q", out)
	}
}

func TestPrepareTakesTheTrailerFromTheEnvironment(t *testing.T) {
	r := newRepo()
	r.env[trailerVariable] = "Co-Authored-By: Someone <someone@example.org>"
	if code, _, errOut := r.run("0.3.0", "x"); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	last := r.calls[len(r.calls)-2] // the commit, before the branch lookup for the message
	if want := "git commit -q -m chore: release 0.3.0 -m Co-Authored-By: Someone <someone@example.org>"; last != want {
		t.Errorf("commit = %q, want %q", last, want)
	}
}

func TestPrepareWithoutAnyTagYet(t *testing.T) {
	r := newRepo()
	r.tags = ""
	if code, _, errOut := r.run("0.3.0", "first"); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestPrepareSkipsTagsThatAreNoReleaseVersions(t *testing.T) {
	r := newRepo()
	r.tags = "vnext\nv0.2.0"
	if code, _, errOut := r.run("0.2.1", "x"); code != exitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	r = newRepo()
	r.tags = "vnext\nv0.2.0"
	if code, _, errOut := r.run("0.2.0", "x"); code != exitFailure || !strings.Contains(errOut, "not newer than the latest tag v0.2.0") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

func TestPrepareRefuses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*repo)
		args  []string
		want  string
	}{
		{"a bad version", func(*repo) {}, []string{"v0.3.0", "x"}, `"v0.3.0" is not a version`},
		{"an empty line", func(*repo) {}, []string{"0.3.0", "  "}, "changelog line is empty"},
		{"main", func(r *repo) { r.branch = "main" }, []string{"0.3.0", "x"}, "refusing to commit on main"},
		{"no branch", func(r *repo) { r.fail["rev-parse"] = errors.New("x") }, []string{"0.3.0", "x"}, "git rev-parse"},
		{"a dirty tree", func(r *repo) { r.status = " M file.go" }, []string{"0.3.0", "x"}, "working tree is not clean"},
		{"a failing status", func(r *repo) { r.fail["status"] = errors.New("x") }, []string{"0.3.0", "x"}, "git status"},
		{"the latest version", func(*repo) {}, []string{"0.2.0", "x"}, "not newer than the latest tag v0.2.0"},
		{"an older version", func(*repo) {}, []string{"0.1.5", "x"}, "not newer than the latest tag v0.2.0"},
		{"failing tags", func(r *repo) { r.fail["tag"] = errors.New("x") }, []string{"0.3.0", "x"}, "git tag"},
		{"an unreadable changelog", func(r *repo) { r.unreadable = true }, []string{"0.3.0", "x"}, "cannot read"},
		{"a version that has an entry", func(r *repo) { r.tags = ""; r.files[changelogFile] = "## 0.3.0\n" }, []string{"0.3.0", "x"}, "already has an entry"},
		{"an unwritable changelog", func(r *repo) { r.unwritable = true }, []string{"0.3.0", "x"}, "cannot write"},
		{"failing checks", func(r *repo) { r.makeErr = errors.New("lint found things") }, []string{"0.3.0", "x"}, "the checks failed"},
		{"a failing add", func(r *repo) { r.fail["add"] = errors.New("x") }, []string{"0.3.0", "x"}, "git add"},
		{"a failing status after the add", func(r *repo) { r.staged = "M  CHANGELOG.md\nM  other.go" }, []string{"0.3.0", "x"}, "unexpected changes besides CHANGELOG.md"},
		{"a failing status after the add (git)", func(r *repo) { r.lateFail["status"] = errors.New("x") }, []string{"0.3.0", "x"}, "git status"},
		{"no branch name after the commit", func(r *repo) { r.lateFail["rev-parse"] = errors.New("x") }, []string{"0.3.0", "x"}, "git rev-parse"},
		{"a failing commit", func(r *repo) { r.fail["commit"] = errors.New("x") }, []string{"0.3.0", "x"}, "git commit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRepo()
			tc.setup(r)
			code, _, errOut := r.run(tc.args...)
			if code != exitFailure || !strings.Contains(errOut, tc.want) {
				t.Fatalf("exit %d, stderr %q; want a failure naming %q", code, errOut, tc.want)
			}
		})
	}
}

func TestPrepareWritesNothingWhenItRefusesBeforeTheChangelog(t *testing.T) {
	r := newRepo()
	r.branch = "main"
	r.run("0.3.0", "x")
	if r.files[changelogFile] != changelogWithTwoVersions {
		t.Error("the changelog was changed although the command refused")
	}
}

func TestTagTagsMainWhenTheEntryIsThere(t *testing.T) {
	r := newRepo()
	r.branch = "main"
	code, out, errOut := r.run("--tag", "0.2.0")
	if code != exitOK || errOut != "" {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if got := r.calls[len(r.calls)-1]; got != "git tag -a v0.2.0 -m v0.2.0" {
		t.Errorf("last call = %q", got)
	}
	if !strings.HasPrefix(out, "tagged v0.2.0 locally (not pushed") {
		t.Errorf("stdout = %q", out)
	}
}

func TestTagRefuses(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*repo)
		ver   string
		want  string
	}{
		{"a bad version", func(r *repo) { r.branch = "main" }, "0.2", "is not a version"},
		{"a branch", func(*repo) {}, "0.2.0", "tag from main after the squash merge (on feature)"},
		{"no branch", func(r *repo) { r.fail["rev-parse"] = errors.New("x") }, "0.2.0", "git rev-parse"},
		{"an unreadable changelog", func(r *repo) { r.branch = "main"; r.unreadable = true }, "0.2.0", "cannot read"},
		{"a missing entry", func(r *repo) { r.branch = "main" }, "0.9.0", "has no '## 0.9.0' entry"},
		{"a failing tag", func(r *repo) { r.branch = "main"; r.fail["tag"] = errors.New("x") }, "0.2.0", "git tag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRepo()
			tc.setup(r)
			code, _, errOut := r.run("--tag", tc.ver)
			if code != exitFailure || !strings.Contains(errOut, tc.want) {
				t.Fatalf("exit %d, stderr %q; want a failure naming %q", code, errOut, tc.want)
			}
		})
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"0.3.0"}, {"--tag"}, {"a", "b", "c"}} {
		code, _, errOut := newRepo().run(args...)
		if code != exitUsage || !strings.Contains(errOut, "usage: release") {
			t.Errorf("run(%v) = %d, %q", args, code, errOut)
		}
	}
}

// With the real git and make, in a repository of its own: the whole preparation and the tag.
func TestAgainstARealRepository(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "T", "GIT_AUTHOR_EMAIL": "t@example.org", "GIT_COMMITTER_NAME": "T",
		"GIT_COMMITTER_EMAIL": "t@example.org", "GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_SYSTEM": os.DevNull, trailerVariable: "Co-Authored-By: A <a@example.org>"} {
		t.Setenv(k, v)
	}
	git := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	write(changelogFile, changelogWithTwoVersions)
	write("Makefile", "cover lint vuln:\n\t@echo check $@\n")
	git("add", ".")
	git("commit", "-q", "-m", "start")
	git("tag", "-a", "v0.2.0", "-m", "v0.2.0")
	git("switch", "-q", "-c", "chore/release")

	var out, errOut bytes.Buffer
	if code := run([]string{"0.3.0", "three"}, systemTools(&out, &errOut), &errOut); code != exitOK {
		t.Fatalf("prepare: exit %d: %s", code, errOut.String())
	}
	if msg := git("log", "-1", "--format=%B"); msg != "chore: release 0.3.0\n\nCo-Authored-By: A <a@example.org>" {
		t.Errorf("commit message = %q", msg)
	}
	if files := git("show", "--name-only", "--format=", "HEAD"); files != changelogFile {
		t.Errorf("the commit holds %q, want only the changelog", files)
	}
	if !strings.Contains(out.String(), "check cover") || !strings.Contains(out.String(), "check vuln") {
		t.Errorf("the checks did not run through make: %q", out.String())
	}

	git("switch", "-q", "main")
	git("merge", "-q", "--ff-only", "chore/release")
	if code := run([]string{"--tag", "0.3.0"}, systemTools(&out, &errOut), &errOut); code != exitOK {
		t.Fatalf("tag: exit %d: %s", code, errOut.String())
	}
	if got := git("tag", "--list", "v0.3.0"); got != "v0.3.0" {
		t.Errorf("tag = %q", got)
	}
}

func TestTheChecksFailThroughMake(t *testing.T) {
	t.Chdir(t.TempDir())
	write := func(text string) {
		if err := os.WriteFile("Makefile", []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("cover lint vuln:\n\t@exit 3\n")
	var out, errOut bytes.Buffer
	if err := systemTools(&out, &errOut).make("cover", "lint", "vuln"); err == nil {
		t.Fatal("a failing make target must be an error")
	}
	if _, err := systemTools(&out, &errOut).git("rev-parse", "--abbrev-ref", "HEAD"); err == nil { // not a repository
		t.Fatal("git outside a repository must be an error")
	}
}
