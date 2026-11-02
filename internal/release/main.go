// Command release cuts a version of the library. It is a build tool of this repository, not part of
// the API, and `scripts/release.sh` runs it.
//
//	go run ./internal/release X.Y.Z "what changed for users"   (on a branch)
//	go run ./internal/release --tag X.Y.Z                      (on main, after the squash merge)
//
// The first form adds the CHANGELOG.md entry, runs the checks of RELEASING.md (`make cover lint vuln`)
// and commits only CHANGELOG.md as `chore: release X.Y.Z`. It refuses to run on main, on a dirty tree,
// and for a version that is not newer than the latest tag. The second form tags the current main commit
// locally. It never pushes: pushing a tag publishes, and a maintainer decides that (RELEASING.md).
//
// The commit message carries a trailer only when the environment names one: set RELEASE_COMMIT_TRAILER
// to the whole line (for example "Co-Authored-By: Name <name@example.org>").
//
// Versions: while the module is 0.x, a change that breaks the exported API or adds to it is a minor
// version, a fix a patch. From 1.0.0 a break is a major version. `make apidiff` lists the API changes.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Exit codes of the command.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

const (
	// changelogFile is the file that gets an entry for each version.
	changelogFile = "CHANGELOG.md"
	// mainBranch is where releases are tagged from and never prepared on.
	mainBranch = "main"
	// trailerVariable names the environment variable that holds the commit trailer, if any.
	trailerVariable = "RELEASE_COMMIT_TRAILER"
	usage           = `usage: release X.Y.Z "changelog line" | release --tag X.Y.Z`
)

// version is a semantic version without a pre-release part.
type version struct{ major, minor, patch int }

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// parseVersion reads "X.Y.Z" (no "v", no leading zeros, no suffix).
func parseVersion(s string) (version, error) {
	m := versionPattern.FindStringSubmatch(s)
	if m == nil {
		return version{}, fmt.Errorf("%q is not a version X.Y.Z", s)
	}
	var v version
	for i, dst := range []*int{&v.major, &v.minor, &v.patch} {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return version{}, fmt.Errorf("%q is not a version X.Y.Z: %w", s, err)
		}
		*dst = n
	}
	return v, nil
}

func (v version) String() string { return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch) }

// newerThan reports whether v is a later version than o.
func (v version) newerThan(o version) bool {
	switch {
	case v.major != o.major:
		return v.major > o.major
	case v.minor != o.minor:
		return v.minor > o.minor
	}
	return v.patch > o.patch
}

// insertEntry puts the entry for ver above the newest one, below the title and the introduction (at
// the end when there is none yet). It refuses a version that has an entry already.
func insertEntry(changelog, ver, line string) (string, error) {
	if hasEntry(changelog, ver) {
		return "", fmt.Errorf("%s already has an entry '## %s'", changelogFile, ver)
	}
	entry := fmt.Sprintf("\n## %s\n\n- %s\n", ver, line)
	i := strings.Index(changelog, "\n## ")
	if i < 0 {
		return strings.TrimRight(changelog, "\n") + "\n" + entry, nil
	}
	return changelog[:i] + entry + changelog[i:], nil
}

// hasEntry reports whether the changelog has a heading "## ver" of its own.
func hasEntry(changelog, ver string) bool {
	for _, l := range strings.Split(changelog, "\n") {
		if l == "## "+ver {
			return true
		}
	}
	return false
}

// tools is what the command needs from the machine: git and make in the repository, and its files.
type tools struct {
	git       func(args ...string) (string, error)
	make      func(targets ...string) error
	readFile  func(name string) ([]byte, error)
	writeFile func(name string, data []byte) error
	getenv    func(string) string
	stdout    io.Writer
}

// main is the process entry point.
//
// coverage: ignore - process entry point; run holds all the logic and is tested.
func main() {
	os.Exit(run(os.Args[1:], systemTools(os.Stdout, os.Stderr), os.Stderr))
}

// systemTools runs the real git and make in the current directory.
func systemTools(stdout, stderr io.Writer) tools {
	return tools{
		git: func(args ...string) (string, error) {
			out, err := exec.CommandContext(context.Background(), "git", args...).CombinedOutput() //nolint:gosec // git with arguments built by this program
			return strings.TrimRight(string(out), "\n"), err
		},
		make: func(targets ...string) error {
			cmd := exec.CommandContext(context.Background(), "make", targets...) //nolint:gosec // make with the targets named in this program
			cmd.Stdout, cmd.Stderr = stdout, stderr
			return cmd.Run()
		},
		readFile:  os.ReadFile,
		writeFile: func(name string, data []byte) error { return os.WriteFile(name, data, 0o644) }, //nolint:gosec // a tracked text file
		getenv:    os.Getenv,
		stdout:    stdout,
	}
}

// run is the whole command: args are the command line without the program name.
func run(args []string, t tools, stderr io.Writer) int {
	var err error
	switch {
	case len(args) == 2 && args[0] == "--tag":
		err = tag(t, args[1])
	case len(args) == 2 && args[0] != "--tag":
		err = prepare(t, args[0], args[1])
	default:
		_, _ = fmt.Fprintln(stderr, usage)
		return exitUsage
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "release:", err)
		return exitFailure
	}
	return exitOK
}

// branch is the current branch.
func (t tools) branch() (string, error) {
	b, err := t.git("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse: %w: %s", err, b)
	}
	return b, nil
}

// tag tags the current main commit with v<ver>, locally.
func tag(t tools, ver string) error {
	if _, err := parseVersion(ver); err != nil {
		return err
	}
	b, err := t.branch()
	if err != nil {
		return err
	}
	if b != mainBranch {
		return fmt.Errorf("tag from main after the squash merge (on %s)", b)
	}
	text, err := t.readFile(changelogFile)
	if err != nil {
		return err
	}
	if !hasEntry(string(text), ver) {
		return fmt.Errorf("%s has no '## %s' entry; is the release PR merged and pulled?", changelogFile, ver)
	}
	if out, err := t.git("tag", "-a", "v"+ver, "-m", "v"+ver); err != nil {
		return fmt.Errorf("git tag: %w: %s", err, out)
	}
	_, _ = fmt.Fprintf(t.stdout, "tagged v%s locally (not pushed; pushing a tag is what publishes, a maintainer decides)\n", ver)
	return nil
}

// prepare adds the CHANGELOG entry for ver, runs the checks and commits the file.
func prepare(t tools, ver, line string) error {
	v, err := parseVersion(ver)
	if err != nil {
		return err
	}
	if strings.TrimSpace(line) == "" {
		return errors.New("the changelog line is empty")
	}
	if err := t.cleanBranch(); err != nil {
		return err
	}
	if err := t.newerThanLatest(v); err != nil {
		return err
	}
	text, err := t.readFile(changelogFile)
	if err != nil {
		return err
	}
	updated, err := insertEntry(string(text), ver, strings.TrimSpace(line))
	if err != nil {
		return err
	}
	if err := t.writeFile(changelogFile, []byte(updated)); err != nil {
		return err
	}
	// The checks of RELEASING.md step 1. The pre-commit hook runs them again on the commit.
	if err := t.make("cover", "lint", "vuln"); err != nil {
		return fmt.Errorf("the checks failed (%s keeps the new entry; fix and rerun the checks, or restore it): %w", changelogFile, err)
	}
	return t.commit(ver)
}

// cleanBranch refuses main and a working tree with changes.
func (t tools) cleanBranch() error {
	b, err := t.branch()
	if err != nil {
		return err
	}
	if b == mainBranch {
		return errors.New("refusing to commit on main: create a branch first")
	}
	status, err := t.git("status", "--porcelain")
	if err != nil {
		return fmt.Errorf("git status: %w: %s", err, status)
	}
	if status != "" {
		return errors.New("the working tree is not clean; commit or stash first")
	}
	return nil
}

// newerThanLatest refuses a version that is not newer than the latest tag, if there is one.
func (t tools) newerThanLatest(v version) error {
	out, err := t.git("tag", "--list", "v*", "--sort=-v:refname")
	if err != nil {
		return fmt.Errorf("git tag: %w: %s", err, out)
	}
	for _, name := range strings.Split(out, "\n") {
		latest, err := parseVersion(strings.TrimPrefix(name, "v"))
		if err != nil {
			continue // a tag that is no release version
		}
		if !v.newerThan(latest) {
			return fmt.Errorf("version %s is not newer than the latest tag v%s", v, latest)
		}
		return nil
	}
	return nil
}

// commit commits CHANGELOG.md alone, with the trailer the environment names, if any.
func (t tools) commit(ver string) error {
	if out, err := t.git("add", changelogFile); err != nil {
		return fmt.Errorf("git add: %w: %s", err, out)
	}
	staged, err := t.git("status", "--porcelain")
	if err != nil {
		return fmt.Errorf("git status: %w: %s", err, staged)
	}
	if staged != "M  "+changelogFile {
		return fmt.Errorf("unexpected changes besides %s; look at git status", changelogFile)
	}
	args := []string{"commit", "-q", "-m", "chore: release " + ver}
	if trailer := t.getenv(trailerVariable); trailer != "" {
		args = append(args, "-m", trailer)
	}
	if out, err := t.git(args...); err != nil {
		return fmt.Errorf("git commit: %w: %s", err, out)
	}
	branch, err := t.branch()
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(t.stdout, "committed the %s entry on %s; push the branch, open a PR, squash merge, then: scripts/release.sh --tag %s\n", ver, branch, ver)
	return nil
}
