package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testEnvironment is a command environment over the ideal make, with the output kept.
func testEnvironment(t *testing.T, m *idealMake) (environment, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := repoRoot(t)
	m.root = root
	e := environment{
		stdout: &stdout, stderr: &stderr,
		getenv:     func(string) string { return "" },
		executable: func() (string, error) { return "/bin/recipecheck", nil },
		abs:        func(string) (string, error) { return root, nil },
		newRunner:  func(string, string) runner { return m.runner() },
		files:      osFiles{},
	}
	return e, &stdout, &stderr
}

func TestRunSaysTheRecipesAreAsPinned(t *testing.T) {
	e, stdout, stderr := testEnvironment(t, newIdealMake(t))
	if code := run(context.Background(), nil, e); code != exitOK || stderr.Len() != 0 || !strings.Contains(stdout.String(), "as pinned") {
		t.Errorf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestRunReportsEachProblemAndExitsOne(t *testing.T) {
	m := newIdealMake(t)
	m.exitStatus = func(tc exitCase, status int, ran bool) (int, bool) { return 1 - status, ran }
	e, stdout, stderr := testEnvironment(t, m)
	code := run(context.Background(), []string{"-root", "somewhere"}, e)
	if code != exitProblems || stdout.Len() != 0 || strings.Count(stderr.String(), "recipecheck: ") != len(exitCases()) {
		t.Errorf("code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestRunGivesTheMakeProgramAndTheRootToTheRunner(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		makeEnv  string
		wantMake string
	}{
		{"the default", nil, "", "make"},
		{"from MAKE", nil, "gmake", "gmake"},
		{"from the flag, which wins", []string{"-make", "/opt/make"}, "gmake", "/opt/make"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _, _ := testEnvironment(t, newIdealMake(t))
			e.getenv = func(name string) string { return map[string]string{"MAKE": tt.makeEnv}[name] }
			var gotRoot, gotMake string
			e.newRunner = func(rootDir, makeBin string) runner {
				gotRoot, gotMake = rootDir, makeBin
				ideal := newIdealMake(t)
				ideal.root = rootDir
				return ideal.runner()
			}
			base := repoRoot(t)
			e.abs = func(p string) (string, error) { return filepath.Join(base, p), nil }
			fillRepository(t, filepath.Join(base, "repo"))
			if code := run(context.Background(), append(tt.args, "-root", "repo"), e); code != exitOK {
				t.Fatalf("code %d", code)
			}
			if gotRoot != filepath.Join(base, "repo") || gotMake != tt.wantMake {
				t.Errorf("root %q, make %q", gotRoot, gotMake)
			}
		})
	}
}

func TestRunExitsTwoWhenItCannotRun(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name string
		args []string
		edit func(e *environment)
		want string
	}{
		{"a flag it does not know", []string{"-nope"}, func(*environment) {}, "flag provided but not defined"},
		{"no executable", nil, func(e *environment) { e.executable = func() (string, error) { return "", boom } }, "boom"},
		{"no absolute root", nil, func(e *environment) { e.abs = func(string) (string, error) { return "", boom } }, "boom"},
		{"a make that cannot run", nil, func(e *environment) {
			e.newRunner = func(string, string) runner {
				return func(context.Context, []string, ...string) (string, int, error) { return "", 0, boom }
			}
		}, "boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, stdout, stderr := testEnvironment(t, newIdealMake(t))
			tt.edit(&e)
			if code := run(context.Background(), tt.args, e); code != exitError || stdout.Len() != 0 || !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("code %d, stdout %q, stderr %q", code, stdout, stderr)
			}
		})
	}
}

func TestRunWithTheStandInArgumentIsTheStandIn(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	log := filepath.Join(dir, "log")
	e, _, stderr := testEnvironment(t, newIdealMake(t))
	e.getenv = func(name string) string { return map[string]string{"STANDIN_LOG": log}[name] }
	if code := run(context.Background(), []string{standinCommand, "--diff", "origin/main"}, e); code != exitOK || stderr.Len() != 0 {
		t.Fatalf("code %d, stderr %q", code, stderr)
	}
	if data, err := os.ReadFile(log); err != nil || string(data) != "ran\n" {
		t.Errorf("log %q (%v)", data, err)
	}
}

func TestTheProcessEnvironmentIsTheRealOne(t *testing.T) {
	e := processEnvironment()
	exe, err := e.executable()
	if err != nil || exe == "" || e.getenv("PATH") != os.Getenv("PATH") || e.stdout != os.Stdout || e.stderr != os.Stderr {
		t.Errorf("executable %q (%v)", exe, err)
	}
	if abs, err := e.abs("."); err != nil || !filepath.IsAbs(abs) {
		t.Errorf("abs %q (%v)", abs, err)
	}
	if e.newRunner == nil || e.files == nil {
		t.Error("the runner or the file system is missing")
	}
}

func TestShellQuoteKeepsAPathAsOneWord(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/tmp/recipecheck", `'/tmp/recipecheck'`},
		{"/tmp/with space/x", `'/tmp/with space/x'`},
		{"/tmp/it's", `'/tmp/it'\''s'`},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := shellQuote(tt.in); got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}
