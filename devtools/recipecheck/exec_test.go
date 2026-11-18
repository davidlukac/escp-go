package main

import (
	"context"
	"os"
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

func TestTheRunnerRunsMakeAsAFreshOne(t *testing.T) {
	for _, name := range scrubbed {
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
	for _, name := range scrubbed {
		want := name + "=\n"
		if name == "EFFICACY" {
			want = name + "=90\n" // the case's own value
		}
		if !strings.Contains(out, "\n"+want) {
			t.Errorf("%s is not as the case sets it: %q", name, out)
		}
	}
	if !strings.Contains(out, "\nKEPT=kept\n") {
		t.Errorf("another variable was dropped: %q", out)
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
	if _, _, err := run(ctx, nil, "x"); err != context.Canceled {
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
