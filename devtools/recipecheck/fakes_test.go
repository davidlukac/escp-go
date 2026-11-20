package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain makes the test binary the programs the checks run: the stand-in for gremlins when it is run
// as `recipecheck standin`, and a fake make when RECIPECHECK_FAKE_MAKE is set (it prints its arguments
// and the variables of the environment the runner must scrub, and exits with the status named in
// RECIPECHECK_FAKE_STATUS).
func TestMain(m *testing.M) {
	switch {
	case runAsGoShim():
		os.Exit(goShim())
	case os.Getenv("RECIPECHECK_HELPER") == "listen":
		ctx, stop := listen(context.Background())
		fmt.Println("ready")
		<-ctx.Done()
		stop()
		fmt.Println("canceled")
		os.Exit(0)
	case len(os.Args) > 1 && os.Args[1] == standinCommand:
		os.Exit(runStandin(os.Getenv, os.Stderr))
	case os.Getenv("RECIPECHECK_FAKE_MAKE") != "":
		fmt.Println("args: " + strings.Join(os.Args[1:], " "))
		for _, name := range append(scrubbed, "KEPT") {
			fmt.Printf("%s=%s\n", name, os.Getenv(name))
		}
		os.Exit(map[string]int{"": 0, "3": 3, "0": 0}[os.Getenv("RECIPECHECK_FAKE_STATUS")])
	}
	os.Exit(m.Run())
}

// idealMake answers like a Makefile that is exactly as pinned: for `make -n` the recipe of the case with
// a gremlins line that holds its pins, and for a run with a stand-in the exit status the case wants (the
// stand-in is run, that is its log written, unless the case is refused before gremlins). The deviations
// let a test break one answer.
type idealMake struct {
	t   *testing.T
	dry map[string]dryCase
	run map[string]exitCase
	// root is the repository the check is run over, as the lint recipe prints it.
	root string

	// dryLines, when set, is given the case and the recipe the ideal make prints and returns what to print.
	dryLines func(tc dryCase, lines []string) []string
	// dryStatus is the exit status of make -n.
	dryStatus int
	// exitStatus, when set, is given the case and the ideal status and returns the status and whether the
	// stand-in ran.
	exitStatus func(tc exitCase, status int, ran bool) (int, bool)
	// onRun is called with every call: the environment and the arguments.
	onRun func(env, args []string) error
}

func newIdealMake(t *testing.T) *idealMake {
	t.Helper()
	m := &idealMake{t: t, dry: map[string]dryCase{}, run: map[string]exitCase{}}
	for _, tc := range dryCases() {
		m.dry[key(tc.env, append([]string{"-n"}, tc.args...))] = tc
	}
	for _, tc := range exitCases() {
		m.run[tc.label] = tc
	}
	return m
}

func key(env, args []string) string { return strings.Join(env, " ") + "|" + strings.Join(args, " ") }

// gremlinsLine is a line that runs gremlins and holds the pins.
func gremlinsLine(p pins) string {
	var words []string
	for _, word := range p.has {
		if word != "GOFLAGS=-count=1" { // the prefix has it
			words = append(words, word)
		}
	}
	return gremlinsPrefix + strings.Join(words, " ") + " " + strings.TrimSpace(p.ending)
}

func (m *idealMake) runner() runner {
	return func(ctx context.Context, env []string, args ...string) (string, int, error) {
		if m.onRun != nil {
			if err := m.onRun(env, args); err != nil {
				return "", 0, err
			}
		}
		if tc, ok := m.dry[key(env, args)]; ok {
			return m.printDry(tc), m.dryStatus, nil
		}
		return m.runExit(env, args)
	}
}

// printDry is what make -n prints for the case.
func (m *idealMake) printDry(tc dryCase) string {
	var lines []string
	switch {
	case tc.target == "lint":
		lines = append([]string{"go build", lintRecipe + m.root}, tc.pins.has...)
	case tc.want == nil:
		lines = []string{"mkdir -p build", gremlinsLine(tc.pins)}
	default:
		for _, line := range tc.want {
			if line == gremlinsPlace {
				line = gremlinsLine(tc.pins)
			}
			lines = append(lines, line)
		}
	}
	if m.dryLines != nil {
		lines = m.dryLines(tc, lines)
	}
	return strings.Join(lines, "\n") + "\n"
}

// runExit answers a run with the stand-in: the arguments of the case, then the hook and GREMLINS=false.
func (m *idealMake) runExit(env, args []string) (string, int, error) {
	m.t.Helper()
	if len(args) < 3 || args[len(args)-1] != "GREMLINS=false" {
		m.t.Fatalf("a run with the stand-in without GREMLINS=false: %v", args)
	}
	var label string
	for _, entry := range env {
		if v, ok := strings.CutPrefix(entry, caseVariable+"="); ok {
			label = v
		}
	}
	tc, ok := m.run[label]
	if !ok {
		m.t.Fatalf("no case for the run %v (%q)", args, label)
	}
	if got := strings.Join(args[:len(args)-2], " "); got != strings.Join(tc.args, " ") {
		m.t.Fatalf("%s: the arguments are %q", tc.label, got)
	}
	hook := args[len(args)-2]
	wantHook := "MUTATE="
	if tc.args[0] == targetExtended {
		wantHook = "MUTATE_EXTENDED="
	}
	if !strings.HasPrefix(hook, wantHook) || !strings.HasSuffix(hook, " "+standinCommand) {
		m.t.Fatalf("%s: the hook is %q", tc.label, hook)
	}
	status, ran := 0, true
	switch tc.want {
	case fails:
		status = 1
	case refuses:
		status, ran = 1, false
	case succeeds:
	}
	if m.exitStatus != nil {
		status, ran = m.exitStatus(tc, status, ran)
	}
	if ran {
		for _, entry := range env {
			if log, ok := strings.CutPrefix(entry, "STANDIN_LOG="); ok {
				if err := os.WriteFile(log, []byte("ran\n"), 0o600); err != nil {
					m.t.Fatal(err)
				}
			}
		}
	}
	return "", status, nil
}

// goodMakefile is the part of the Makefile the check reads for itself.
var goodMakefile = "all:\n\ttrue\n\n" + recipeTarget + ":\n" + recipeSource + "\n\n" +
	mutantsTarget + ":\n" + strings.Join(mutantsSource, "\n") + "\n\n" +
	deadcodeTarget + ":\n" + strings.Join(deadcodeSource, "\n") + "\n\n" +
	zizmorTarget + ":\n" + strings.Join(zizmorSource, "\n") + "\n\nnext:\n\ttrue\n"

// repoRoot is a scratch repository: a directory with the Makefile the check reads.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(goodMakefile), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// failingFiles is the real file system that fails an operation on a name when fail says so.
type failingFiles struct {
	osFiles
	fail func(op, name string) error
	// temps collects the scratch directories made.
	temps *[]string
}

func (f failingFiles) err(op, name string) error {
	if f.fail == nil {
		return nil
	}
	return f.fail(op, name)
}

func (f failingFiles) MkdirTemp(dir, pattern string) (string, error) {
	if err := f.err("MkdirTemp", pattern); err != nil {
		return "", err
	}
	made, err := f.osFiles.MkdirTemp(dir, pattern)
	if err == nil && f.temps != nil {
		*f.temps = append(*f.temps, made)
	}
	return made, err
}

func (f failingFiles) MkdirAll(path string, perm os.FileMode) error {
	if err := f.err("MkdirAll", path); err != nil {
		return err
	}
	return f.osFiles.MkdirAll(path, perm)
}

func (f failingFiles) Stat(name string) (os.FileInfo, error) {
	if err := f.err("Stat", name); err != nil {
		return nil, err
	}
	return f.osFiles.Stat(name)
}

func (f failingFiles) ReadFile(name string) ([]byte, error) {
	if err := f.err("ReadFile", name); err != nil {
		return nil, err
	}
	return f.osFiles.ReadFile(name)
}

func (f failingFiles) WriteFile(name string, data []byte, perm os.FileMode) error {
	if err := f.err("WriteFile", name); err != nil {
		return err
	}
	return f.osFiles.WriteFile(name, data, perm)
}

func (f failingFiles) Remove(name string) error {
	if err := f.err("Remove", name); err != nil {
		return err
	}
	return f.osFiles.Remove(name)
}

// The recipes `go run ./internal/mutationreport`, which compiles and links the tool at every step: for
// the many runs of this test a copy of the test binary stands in as `go` on the PATH and runs the tool
// built once. Any other `go` command goes to the real one.
const shimVariable = "RECIPECHECK_GO_SHIM"

// runAsGoShim says whether this process was started as the stand-in `go`.
func runAsGoShim() bool {
	return os.Getenv(shimVariable) != "" && filepath.Base(os.Args[0]) == "go"
}

// goShim is the stand-in `go`: it runs the report tool or the real go and returns the exit status.
func goShim() int {
	args := os.Args[1:]
	program, rest := os.Getenv("RECIPECHECK_REAL_GO"), args
	if len(args) >= 2 && args[0] == "run" && args[1] == "./internal/mutationreport" {
		program, rest = os.Getenv("RECIPECHECK_REPORT_TOOL"), args[2:]
	}
	cmd := exec.Command(program, rest...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		return exitErr.ExitCode()
	}
	if err != nil {
		return 127
	}
	return 0
}
