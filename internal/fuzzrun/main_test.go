package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeTool answers from tables and records what it was asked to fuzz.
type fakeTool struct {
	pkgs       []string
	pkgsErr    error
	targetsOf  map[string][]string
	targetsErr map[string]error
	failing    map[string]bool // "pkg target" that fail
	ran        []string
	durations  []time.Duration
}

func (f *fakeTool) packages(context.Context) ([]string, error) { return f.pkgs, f.pkgsErr }

func (f *fakeTool) targets(_ context.Context, pkg string) ([]string, error) {
	return f.targetsOf[pkg], f.targetsErr[pkg]
}

func (f *fakeTool) fuzz(_ context.Context, pkg, target string, d time.Duration, out io.Writer) error {
	f.ran = append(f.ran, pkg+" "+target)
	f.durations = append(f.durations, d)
	_, _ = io.WriteString(out, "fuzzing "+target+"\n")
	if f.failing[pkg+" "+target] {
		return errors.New("a failing input was found")
	}
	return nil
}

func runWith(t *testing.T, tool *fakeTool, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = run(context.Background(), args, &o, &e, tool)
	return code, o.String(), e.String()
}

func TestEveryTargetOfEveryPackageRunsForTheDefaultTimeAndTheOutputNamesIt(t *testing.T) {
	tool := &fakeTool{
		pkgs:      []string{"m", "m/wire", "m/none"},
		targetsOf: map[string][]string{"m": {"FuzzA"}, "m/wire": {"FuzzB", "FuzzC"}},
	}
	code, out, errOut := runWith(t, tool)
	if code != exitOK || errOut != "" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if want := []string{"m FuzzA", "m/wire FuzzB", "m/wire FuzzC"}; !reflect.DeepEqual(tool.ran, want) {
		t.Errorf("ran %v, want %v", tool.ran, want)
	}
	for _, d := range tool.durations {
		if d != 30*time.Second {
			t.Errorf("fuzzed for %v, want the default 30s", d)
		}
	}
	for _, want := range []string{"== m FuzzA (30s)\n", "== m/wire FuzzC (30s)\n", "fuzzing FuzzB\n"} {
		t.Run(want, func(t *testing.T) {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q:\n%s", want, out)
			}
		})
	}
}

func TestTheSecondsOnTheCommandLineSetTheTimeOfEachTarget(t *testing.T) {
	tool := &fakeTool{pkgs: []string{"m"}, targetsOf: map[string][]string{"m": {"FuzzA"}}}
	if code, _, errOut := runWith(t, tool, "5"); code != exitOK || errOut != "" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if len(tool.durations) != 1 || tool.durations[0] != 5*time.Second {
		t.Errorf("durations %v, want one of 5s", tool.durations)
	}
}

func TestAFailingTargetFailsTheRunButTheOthersStillRun(t *testing.T) {
	tool := &fakeTool{
		pkgs:      []string{"m"},
		targetsOf: map[string][]string{"m": {"FuzzA", "FuzzB"}},
		failing:   map[string]bool{"m FuzzA": true},
	}
	code, _, errOut := runWith(t, tool)
	if code != exitFailure || !strings.Contains(errOut, "m FuzzA failed: a failing input was found") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if want := []string{"m FuzzA", "m FuzzB"}; !reflect.DeepEqual(tool.ran, want) {
		t.Errorf("ran %v, want both targets %v", tool.ran, want)
	}
}

func TestAPackageThatCannotBeListedFailsTheRunAndTheNextPackageStillRuns(t *testing.T) {
	tool := &fakeTool{
		pkgs:       []string{"bad", "m"},
		targetsOf:  map[string][]string{"m": {"FuzzA"}},
		targetsErr: map[string]error{"bad": errors.New("build failed")},
	}
	code, _, errOut := runWith(t, tool)
	if code != exitFailure || !strings.Contains(errOut, "listing the tests of bad: build failed") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if want := []string{"m FuzzA"}; !reflect.DeepEqual(tool.ran, want) {
		t.Errorf("ran %v, want %v", tool.ran, want)
	}
}

func TestWhenThePackagesCannotBeListedNothingRuns(t *testing.T) {
	tool := &fakeTool{pkgsErr: errors.New("no go.mod")}
	code, _, errOut := runWith(t, tool)
	if code != exitFailure || !strings.Contains(errOut, "listing the packages: no go.mod") || len(tool.ran) != 0 {
		t.Fatalf("code %d, stderr %q, ran %v", code, errOut, tool.ran)
	}
}

func TestAnythingButASecondsNumberIsAUsageError(t *testing.T) {
	for _, args := range [][]string{{"x"}, {"0"}, {"-3"}, {"1.5"}, {"5", "6"}} {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			tool := &fakeTool{pkgs: []string{"m"}, targetsOf: map[string][]string{"m": {"FuzzA"}}}
			code, _, errOut := runWith(t, tool, args...)
			if code != exitUsage || errOut == "" || len(tool.ran) != 0 {
				t.Errorf("%v: code %d, stderr %q, ran %v; want a usage error and no run", args, code, errOut, tool.ran)
			}
		})
	}
}

func TestOnlyTheFuzzTargetsAreTakenFromAListing(t *testing.T) {
	listing := "FuzzParse\nFuzzDecode\nTestNotFuzz\nBenchmarkFuzzy\nok  \tgithub.com/x/y\t0.010s\n"
	want := []string{"FuzzParse", "FuzzDecode"}
	if got := fuzzTargets(listing); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := fuzzTargets("no tests to run\n"); len(got) != 0 {
		t.Errorf("got %v from a listing without targets", got)
	}
}

// helperEnv makes the test binary act as the fake "go" command instead of running tests.
const helperEnv = "FUZZRUN_TEST_HELPER"

// TestMain lets the test binary stand in for the go tool: execTool starts it with the arguments it
// would give to go, and the helper prints what a go command would, or fails.
func TestMain(m *testing.M) {
	if mode := os.Getenv(helperEnv); mode != "" {
		os.Exit(helperProcess(mode, os.Args[1:]))
	}
	os.Exit(m.Run())
}

func helperProcess(mode string, args []string) int {
	switch mode {
	case "echo": // print the arguments, one line, like a program that reports what it was asked
		fmt.Println(strings.Join(args, " "))
	case "list-packages":
		fmt.Println("m\nm/wire")
	case "list-tests":
		fmt.Println("FuzzParse\nTestOther\nok  \tm\t0.01s")
	case "fail":
		fmt.Fprintln(os.Stderr, "boom")
		return 3
	}
	return 0
}

// helperCommand is execTool.command for the tests: it starts this binary as the helper in mode.
func helperCommand(mode string) func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], append([]string{name}, args...)...)
		cmd.Env = append(os.Environ(), helperEnv+"="+mode)
		return cmd
	}
}

func TestTheGoToolListsThePackagesAndTheFuzzTargets(t *testing.T) {
	ctx := context.Background()
	pkgs, err := execTool{command: helperCommand("list-packages")}.packages(ctx)
	if err != nil || !reflect.DeepEqual(pkgs, []string{"m", "m/wire"}) {
		t.Errorf("packages = %v, %v", pkgs, err)
	}
	targets, err := execTool{command: helperCommand("list-tests")}.targets(ctx, "m")
	if err != nil || !reflect.DeepEqual(targets, []string{"FuzzParse"}) {
		t.Errorf("targets = %v, %v", targets, err)
	}
}

func TestTheGoToolIsAskedForTheExactTargetAndTheTime(t *testing.T) {
	var out bytes.Buffer
	if err := (execTool{command: helperCommand("echo")}).fuzz(context.Background(), "m/wire", "FuzzParse", 7*time.Second, &out); err != nil {
		t.Fatal(err)
	}
	want := "go test m/wire -run ^$ -fuzz ^FuzzParse$ -fuzztime 7s\n"
	if out.String() != want {
		t.Errorf("the go tool was started as %q, want %q", out.String(), want)
	}
}

func TestAFailingGoToolIsReported(t *testing.T) {
	ctx := context.Background()
	tool := execTool{command: helperCommand("fail")}
	if _, err := tool.packages(ctx); err == nil {
		t.Error("a failing go list was not reported")
	}
	if _, err := tool.targets(ctx, "m"); err == nil {
		t.Error("a failing go test -list was not reported")
	}
	var out bytes.Buffer
	if err := tool.fuzz(ctx, "m", "FuzzParse", time.Second, &out); err == nil || !strings.Contains(out.String(), "boom") {
		t.Errorf("a failing fuzz run: err %v, output %q; want the error and the tool's output", err, out.String())
	}
}
