package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// runner runs make in the repository with the given extra environment and arguments and returns what it
// printed on stdout and its exit status. An error is a make that could not run (not found, killed by a
// canceled context); a make that exits non-zero is not an error.
type runner func(ctx context.Context, env []string, args ...string) (stdout string, status int, err error)

// caseVariable names the case in the environment of a run: make ignores it, a fake make answers by it.
const caseVariable = "RECIPECHECK_CASE"

// File and directory permissions of what the check writes.
const (
	dirMode  = 0o750
	fileMode = 0o600
)

// The reports the stand-in leaves, as gremlins writes them: the first has a mutant killed and one that
// timed out (the efficacy is 100%), the second a mutant killed and one that lived (50%).
const (
	goodReportJSON = `{"files":[{"file_name":"a.go","mutations":[{"type":"X","status":"KILLED","line":1,"column":1},{"type":"X","status":"TIMED OUT","line":2,"column":1}]}]}`
	badReportJSON  = `{"files":[{"file_name":"a.go","mutations":[{"type":"X","status":"KILLED","line":1,"column":1},{"type":"X","status":"LIVED","line":2,"column":1}]}]}`
)

// files is the part of the file system the check touches; the tests make it fail.
type files interface {
	MkdirTemp(dir, pattern string) (string, error)
	MkdirAll(path string, perm os.FileMode) error
	Stat(name string) (os.FileInfo, error)
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm os.FileMode) error
	Remove(name string) error
	RemoveAll(path string) error
	Chmod(name string, mode os.FileMode) error
}

// osFiles is the real file system.
type osFiles struct{}

func (osFiles) MkdirTemp(dir, pattern string) (string, error)        { return os.MkdirTemp(dir, pattern) }
func (osFiles) MkdirAll(path string, perm os.FileMode) error         { return os.MkdirAll(path, perm) }
func (osFiles) Stat(name string) (os.FileInfo, error)                { return os.Stat(name) }
func (osFiles) ReadFile(name string) ([]byte, error)                 { return os.ReadFile(name) } //nolint:gosec // the files the check names
func (osFiles) WriteFile(name string, d []byte, p os.FileMode) error { return os.WriteFile(name, d, p) }
func (osFiles) Remove(name string) error                             { return os.Remove(name) }
func (osFiles) RemoveAll(path string) error                          { return os.RemoveAll(path) }
func (osFiles) Chmod(name string, mode os.FileMode) error            { return os.Chmod(name, mode) }

// checker checks the recipes of the mutation targets of the Makefile in root.
type checker struct {
	root string
	// run runs make; standin is the command line (a shell word list) of the stand-in for gremlins.
	run     runner
	standin string
	files   files
}

// check runs every case and returns what is wrong, one message per problem. The report of the working
// tree (build/mutation.json) is put aside while the exit codes are run and put back, with its
// permissions, whatever happens, a canceled context included. An error means the check could not run to
// the end: a make that cannot be run, or a canceled context.
func (c *checker) check(ctx context.Context) (problems []string, err error) {
	for _, tc := range dryCases() {
		found, err := c.dry(ctx, tc)
		if err != nil {
			return nil, err
		}
		problems = append(problems, found...)
	}
	text, err := c.files.ReadFile(filepath.Join(c.root, "Makefile"))
	if err != nil {
		return nil, fmt.Errorf("reading the Makefile: %w", err)
	}
	problems = append(problems, sourceProblems(string(text))...)
	for _, name := range zizmorWorkflows {
		workflow, err := c.files.ReadFile(filepath.Join(c.root, filepath.FromSlash(name)))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		problems = append(problems, workflowProblems(name, string(workflow))...)
	}
	s, err := c.stage()
	if err != nil {
		return nil, err
	}
	defer s.restore()
	for _, tc := range exitCases() {
		found, err := c.exit(ctx, s, tc)
		if err != nil {
			return nil, err
		}
		problems = append(problems, found...)
	}
	return problems, nil
}

// dry runs `make -n` for the case and compares the recipe it prints with what the case wants.
func (c *checker) dry(ctx context.Context, tc dryCase) ([]string, error) {
	out, status, err := c.run(ctx, tc.env, append([]string{"-n"}, tc.args...)...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", tc.label, err)
	}
	if status != 0 {
		return []string{fmt.Sprintf("%s: make -n exited with status %d", tc.label, status)}, nil
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if tc.target == "lint" {
		return lintProblems(tc, lines, c.root), nil
	}
	if tc.plain {
		if equalLines(tc.want, lines) {
			return nil, nil
		}
		return []string{fmt.Sprintf("%s: the recipe is not what it must be; wanted, then got:\n%s\n%s",
			tc.label, describeLines(tc.want), describeLines(lines))}, nil
	}
	var problems []string
	gremlins, replaced := splitGremlins(lines)
	problems = append(problems, pinProblems(tc, gremlins)...)
	if tc.want != nil && !equalLines(tc.want, replaced) {
		problems = append(problems, fmt.Sprintf("%s: the recipe is not what it must be; wanted, then got:\n%s\n%s",
			tc.label, describeLines(tc.want), describeLines(replaced)))
	}
	return problems, nil
}

// lintProblems says whether `make -n lint` runs this check, the dead-code check and the zizmor probes. For this check: a
// line that is exactly the recipe of the mutationrecipe target with the root of the repository (as given,
// and with its symbolic links resolved, which is what make's own idea of the directory may be), with
// nothing after it (`|| true`, an extra flag). For the dead-code check and the zizmor probes: each line of the
// case's pins.has, whole.
func lintProblems(tc dryCase, lines []string, root string) []string {
	var problems []string
	wanted := []string{lintRecipe + root}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		wanted = append(wanted, lintRecipe+resolved)
	}
	if !slices.ContainsFunc(lines, func(line string) bool { return slices.Contains(wanted, line) }) {
		problems = append(problems, tc.label+": make lint does not run the check as `"+wanted[0]+"` (no line of its recipe is that, whole)")
	}
	for _, want := range tc.pins.has {
		if !slices.Contains(lines, want) {
			problems = append(problems, tc.label+": make lint does not run `"+want+"` (no line of its recipe is that, whole)")
		}
	}
	return problems
}

// sourceProblems pins the recipes of the mutationrecipe, recipemutants, deadcode, zizmorcheck and zizmor targets in the text of the
// Makefile: `make -n` hides a `-` prefix (a failing step that does not fail make) and an `@`, so each
// recipe is compared as it is written, line by line, and must have no other line.
func sourceProblems(makefile string) []string {
	lines := strings.Split(makefile, "\n")
	var problems []string
	for target, want := range map[string][]string{
		recipeTarget:    {recipeSource},
		mutantsTarget:   mutantsSource,
		deadcodeTarget:  deadcodeSource,
		zizmorTarget:    zizmorSource,
		zizmorRunTarget: zizmorRunSource,
	} {
		at := slices.Index(lines, target+":")
		if at < 0 {
			problems = append(problems, "the Makefile has no "+target+" target")
			continue
		}
		var recipe []string
		for _, line := range lines[at+1:] {
			if !strings.HasPrefix(line, "\t") {
				break
			}
			recipe = append(recipe, line)
		}
		if !slices.Equal(recipe, want) {
			problems = append(problems, fmt.Sprintf("the recipe of %s is not as written in the check (a prefix or a suffix on a line, or another line, hides a failure from make): got %q, want %q",
				target, recipe, want))
		}
	}
	slices.Sort(problems)
	return problems
}

// splitGremlins returns the lines that run gremlins and the recipe with the line that starts as the
// gremlins line must (`GOFLAGS=-count=1 go tool ... gremlins unleash . `) written as gremlinsPlace: the
// rest of that line is held to the pins, the line itself varies with PKG and WORKERS.
func splitGremlins(lines []string) (gremlins, replaced []string) {
	replaced = make([]string, len(lines))
	for i, line := range lines {
		replaced[i] = line
		if strings.Contains(line, gremlinsMarker) {
			gremlins = append(gremlins, line)
		}
		if strings.HasPrefix(line, gremlinsPrefix) {
			replaced[i] = gremlinsPlace
		}
	}
	return gremlins, replaced
}

// pinProblems holds the one gremlins line to the pins of the case.
func pinProblems(tc dryCase, gremlins []string) []string {
	if len(gremlins) != 1 {
		return []string{fmt.Sprintf("%s: the recipe runs gremlins %d times, not once", tc.label, len(gremlins))}
	}
	line := gremlins[0]
	var problems []string
	if tc.pins.ending != "" && !strings.HasSuffix(line, tc.pins.ending) {
		problems = append(problems, fmt.Sprintf("%s: gremlins is not run ending with %q", tc.label, tc.pins.ending))
	}
	for _, word := range tc.pins.has {
		if !strings.Contains(" "+line+" ", " "+word+" ") {
			problems = append(problems, fmt.Sprintf("%s: gremlins is not run with %q", tc.label, word))
		}
	}
	for _, word := range append([]string{"--threshold-efficacy"}, tc.pins.lacks...) {
		if strings.Contains(line, word) {
			problems = append(problems, fmt.Sprintf("%s: gremlins is run with %q", tc.label, word))
		}
	}
	return problems
}

// equalLines says whether two lists of lines are the same.
func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// stage is the scratch the exit-code runs need: a directory with the two reports and the log the stand-in
// writes to, and the report of the working tree, put aside.
type stage struct {
	files   files
	dir     string
	root    string
	log     string
	good    string
	bad     string
	saved   []byte
	mode    os.FileMode
	present bool
}

// stage makes the scratch directory and puts the report of the working tree aside.
func (c *checker) stage() (*stage, error) {
	dir, err := c.files.MkdirTemp("", "recipecheck-")
	if err != nil {
		return nil, fmt.Errorf("making the scratch directory: %w", err)
	}
	s := &stage{files: c.files, dir: dir, root: c.root, log: filepath.Join(dir, "standin.log"),
		good: filepath.Join(dir, "good.json"), bad: filepath.Join(dir, "bad.json")}
	for path, content := range map[string]string{s.good: goodReportJSON, s.bad: badReportJSON} {
		if err := s.files.WriteFile(path, []byte(content), fileMode); err != nil {
			s.discard()
			return nil, fmt.Errorf("writing a report: %w", err)
		}
	}
	if err := s.putAside(); err != nil {
		s.discard()
		return nil, err
	}
	return s, nil
}

// reportPath is the report of the working tree.
func (s *stage) reportPath() string { return filepath.Join(s.root, filepath.FromSlash(reportFile)) }

// putAside remembers the report of the working tree, with its permissions, if there is one.
func (s *stage) putAside() error {
	info, err := s.files.Stat(s.reportPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("looking at %s: %w", reportFile, err)
	}
	if s.saved, err = s.files.ReadFile(s.reportPath()); err != nil {
		return fmt.Errorf("reading %s: %w", reportFile, err)
	}
	s.mode, s.present = info.Mode().Perm(), true
	return nil
}

// discard removes the scratch directory and leaves the working tree as it is (nothing was moved yet).
func (s *stage) discard() { _ = s.files.RemoveAll(s.dir) }

// restore puts the report of the working tree back with the permissions it had, or removes the one a
// run left when there was none, and removes the scratch directory.
func (s *stage) restore() {
	s.discard()
	if !s.present {
		_ = s.files.Remove(s.reportPath())
		return
	}
	_ = s.files.WriteFile(s.reportPath(), s.saved, s.mode)
	_ = s.files.Chmod(s.reportPath(), s.mode)
}

// prepare sets the working tree for one run: build/ there (a recipe that removes it is for the run to
// notice), no report in it (the stale good one when asked), the
// stand-in's log empty.
func (s *stage) prepare(tc exitCase) error {
	if err := s.files.MkdirAll(filepath.Join(s.root, "build"), dirMode); err != nil {
		return fmt.Errorf("making build/: %w", err)
	}
	if err := s.files.Remove(s.reportPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing %s: %w", reportFile, err)
	}
	if tc.stale {
		if err := s.files.WriteFile(s.reportPath(), []byte(goodReportJSON), fileMode); err != nil {
			return fmt.Errorf("writing a stale %s: %w", reportFile, err)
		}
	}
	return s.files.WriteFile(s.log, nil, fileMode)
}

// reportFor is the file the stand-in copies to build/mutation.json for a kind of report, or none.
func (s *stage) reportFor(kind string) string {
	switch kind {
	case goodReport:
		return s.good
	case badReport:
		return s.bad
	}
	return ""
}

// exit runs make for the case with the stand-in in place of gremlins (and GREMLINS=false, so that a
// recipe that stops using the stand-in hook and expands $(GREMLINS) fails at once and never starts the
// real tool) and compares what it did with what the case wants.
func (c *checker) exit(ctx context.Context, s *stage, tc exitCase) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.prepare(tc); err != nil {
		return nil, err
	}
	hook := "MUTATE"
	if tc.args[0] == targetExtended {
		hook = "MUTATE_EXTENDED"
	}
	args := append(append([]string{}, tc.args...), hook+"="+c.standin, "GREMLINS=false")
	env := []string{"STANDIN_LOG=" + s.log, "STANDIN_REPORT=" + s.reportFor(tc.report), caseVariable + "=" + tc.label}
	_, status, err := c.run(ctx, env, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", tc.label, err)
	}
	ran, err := s.standinRan()
	if err != nil {
		return nil, err
	}
	switch {
	case tc.want == succeeds && status != 0:
		return []string{tc.label + ": make failed and must succeed"}, nil
	case tc.want != succeeds && status == 0:
		return []string{tc.label + ": make succeeded and must fail"}, nil
	case tc.want == refuses && ran:
		return []string{tc.label + ": make failed, but only after gremlins was started"}, nil
	}
	return nil, nil
}

// standinRan says whether the stand-in left a mark in its log.
func (s *stage) standinRan() (bool, error) {
	data, err := s.files.ReadFile(s.log)
	if err != nil {
		return false, fmt.Errorf("reading the stand-in's log: %w", err)
	}
	return len(data) > 0, nil
}
