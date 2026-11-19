package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// fakeMake makes the runner run this test binary as make (see TestMain).
func fakeMake(t *testing.T, status string) runner {
	t.Helper()
	t.Setenv("RECIPECHECK_FAKE_MAKE", "1")
	t.Setenv("RECIPECHECK_FAKE_STATUS", status)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return execRunner(t.TempDir(), exe)
}

// literalScrubbed is the list of variables a run of make must not inherit, written out here so that
// the list in the program is held to it (a name dropped from it goes unnoticed otherwise): what an outer
// make passes on, what the Makefile reads, the hooks the check overrides, and what the stand-in reads.
var literalScrubbed = []string{
	"MAKEFLAGS", "MFLAGS", "MAKELEVEL",
	"EFFICACY", "TIMEOUT_COEFFICIENT", "WORKERS", "PKG", "BASE", "MUTATE", "MUTATE_EXTENDED", "GREMLINS",
	"STANDIN_LOG", "STANDIN_REPORT",
}

func TestTheScrubListIsTheOneWrittenOutHere(t *testing.T) {
	got := slices.Sorted(slices.Values(scrubbed))
	want := slices.Sorted(slices.Values(literalScrubbed))
	if !slices.Equal(got, want) {
		t.Errorf("scrubbed is %q, want %q", got, want)
	}
}

func TestTheRunnerRunsMakeAsAFreshOne(t *testing.T) {
	for _, name := range literalScrubbed { // subtests: sets up the environment of the one test
		t.Setenv(name, "leaked")
	}
	t.Setenv("KEPT", "kept")
	out, status, err := fakeMake(t, "0")(context.Background(), []string{"EFFICACY=90"}, "-n", "mutation")
	if err != nil || status != 0 {
		t.Fatalf("status %d, error %v", status, err)
	}
	if !strings.HasPrefix(out, "args: --no-print-directory -n mutation\n") {
		t.Errorf("arguments: %q", out)
	}
	for _, name := range literalScrubbed {
		t.Run(name, func(t *testing.T) {
			want := name + "=\n"
			if name == "EFFICACY" {
				want = name + "=90\n" // the case's own value
			}
			if !strings.Contains(out, "\n"+want) {
				t.Errorf("%s is not as the case sets it: %q", name, out)
			}
		})
	}
	if !strings.Contains(out, "\nKEPT=kept\n") {
		t.Errorf("another variable was dropped: %q", out)
	}
}

// makefileInputs are the variables the mutation targets of the real Makefile take from outside: the ones
// it uses (between the mutation-testing comment and the end of mutation-extended) without defining them
// anywhere in the Makefile, or defines with ?=. A new one that the scrub list lacks would leak from a developer's environment
// into the check.
func makefileInputs(t *testing.T) []string {
	t.Helper()
	text, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	whole := string(text)
	region := whole
	start := strings.Index(region, "# Mutation testing (gremlins")
	end := strings.Index(region, "mutation-extended:")
	if start < 0 || end < start {
		t.Fatal("the Makefile has no mutation section")
	}
	end += strings.Index(region[end:], "\n\n")
	region = region[start:end]
	defined := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^([A-Z][A-Z_0-9]*)\s*(:=|\?=|=)`).FindAllStringSubmatch(whole, -1) {
		defined[m[1]] = m[2] != "?="
	}
	used := map[string]bool{}
	for _, m := range regexp.MustCompile(`\$\((?:origin )?([A-Z][A-Z_0-9]*)\)`).FindAllStringSubmatch(region, -1) {
		used[m[1]] = true
	}
	for _, m := range regexp.MustCompile(`\$\(origin ([A-Z][A-Z_0-9]*)\)`).FindAllStringSubmatch(region, -1) {
		used[m[1]] = true
	}
	for name := range defined {
		if strings.Contains(region, name) {
			used[name] = true
		}
	}
	var inputs []string
	for name := range used {
		if internal, isDefined := defined[name]; (!isDefined || !internal) && name != "CURDIR" {
			inputs = append(inputs, name)
		}
	}
	slices.Sort(inputs)
	return inputs
}

func TestEveryVariableTheMutationTargetsTakeFromOutsideIsScrubbed(t *testing.T) {
	inputs := makefileInputs(t)
	if want := []string{"BASE", "EFFICACY", "PKG", "TIMEOUT_COEFFICIENT", "WORKERS"}; !slices.Equal(inputs, want) {
		t.Fatalf("the inputs of the mutation targets are %q, want %q: update this list and the scrub list", inputs, want)
	}
	for _, name := range inputs {
		t.Run(name, func(t *testing.T) {
			if !slices.Contains(scrubbed, name) {
				t.Errorf("%s is read by the Makefile and is not scrubbed from the environment", name)
			}
		})
	}
}

func TestTheRunnerReturnsTheStatusOfMakeAndNotAnError(t *testing.T) {
	out, status, err := fakeMake(t, "3")(context.Background(), nil, "x")
	if err != nil || status != 3 || out == "" {
		t.Errorf("out %q, status %d, error %v", out, status, err)
	}
}

func TestTheRunnerReturnsAnErrorForAMakeThatCannotRun(t *testing.T) {
	_, _, err := execRunner(t.TempDir(), "/nonexistent/make")(context.Background(), nil, "x")
	if err == nil {
		t.Error("no error")
	}
}

func TestTheRunnerReturnsTheContextErrorWhenCanceled(t *testing.T) {
	run := fakeMake(t, "0")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := run(ctx, nil, "x"); !errors.Is(err, context.Canceled) {
		t.Errorf("error %v", err)
	}
}

func TestScrubEnvKeepsWhatTheMakefileDoesNotRead(t *testing.T) {
	got := scrubEnv([]string{"PATH=/bin", "EFFICACY=90", "MAKEFLAGS=-j4", "HOME=/h", "PKG=model", "NOEQUALS"})
	want := []string{"PATH=/bin", "HOME=/h", "NOEQUALS"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
}
