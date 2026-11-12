package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeSystem records what the command does to the machine, in order.
type fakeSystem struct {
	cpu    int
	events []string
	envs   [][]string
	args   [][]string
	failOn func(env []string) bool // makes the tests of the legs it accepts fail
	clock  time.Time
	step   time.Duration
}

func (f *fakeSystem) cpus() int { return f.cpu }

func (f *fakeSystem) now() time.Time {
	f.clock = f.clock.Add(f.step)
	return f.clock
}

func (f *fakeSystem) goTest(_ context.Context, env, args []string, out io.Writer) error {
	f.events = append(f.events, "tests")
	f.envs = append(f.envs, env)
	f.args = append(f.args, args)
	_, _ = io.WriteString(out, "go test output\n")
	if f.failOn != nil && f.failOn(env) {
		return errors.New("exit status 1")
	}
	return nil
}

func (f *fakeSystem) load(n int) func() {
	f.events = append(f.events, "load "+string(rune('0'+n)))
	return func() { f.events = append(f.events, "unload") }
}

func runWith(t *testing.T, sys *fakeSystem, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = run(context.Background(), args, &o, &e, sys)
	return code, o.String(), e.String()
}

func hasEnv(env []string, kv string) bool {
	return slices.Contains(env, kv)
}

func TestWithoutArgumentsEveryLegRunsTheWholeSuiteInOrder(t *testing.T) {
	sys := &fakeSystem{cpu: 4, step: 90 * time.Second}
	code, out, errOut := runWith(t, sys)
	if code != exitOK || errOut != "" {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	// idle, single, then loaded: the load starts before its tests and ends after them.
	if want := []string{"tests", "tests", "load 4", "tests", "unload"}; !reflect.DeepEqual(sys.events, want) {
		t.Errorf("events %v, want %v", sys.events, want)
	}
	wantArgs := []string{"test", "-race", "-shuffle=on", "-count=3", "./..."}
	for i, a := range sys.args {
		if !reflect.DeepEqual(a, wantArgs) {
			t.Errorf("leg %d ran go %v, want go %v", i, a, wantArgs)
		}
	}
	for _, want := range []string{"== soak idle:", "== soak single:", "== soak loaded:", "== 4 busy goroutines beside the tests", "== soak loaded took 1m30s"} {
		t.Run(want, func(t *testing.T) {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q:\n%s", want, out)
			}
		})
	}
}

func TestOnlyTheSingleLegRunsWithOneProcessorAndEveryLegSoaksTheLeakTest(t *testing.T) {
	sys := &fakeSystem{cpu: 2}
	if code, _, errOut := runWith(t, sys); code != exitOK {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	tests := []struct {
		name       string
		leg        int
		gomaxprocs bool
	}{
		{name: "idle", leg: 0},
		{name: "single", leg: 1, gomaxprocs: true},
		{name: "loaded", leg: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := sys.envs[tc.leg]
			if got := hasEnv(env, "GOMAXPROCS=1"); got != tc.gomaxprocs {
				t.Errorf("GOMAXPROCS=1 in %v: %v, want %v", env, got, tc.gomaxprocs)
			}
			if !hasEnv(env, "ESCP_SOAK=200") {
				t.Errorf("the leak test is not soaked: %v", env)
			}
		})
	}
}

func TestTheLoadStopsWhenTheTestsFailAndTheOtherLegsStillRun(t *testing.T) {
	sys := &fakeSystem{cpu: 3, failOn: func(env []string) bool { return !hasEnv(env, "GOMAXPROCS=1") }}
	code, _, errOut := runWith(t, sys)
	if code != exitFailure {
		t.Errorf("code %d, want %d", code, exitFailure)
	}
	if want := []string{"tests", "tests", "load 3", "tests", "unload"}; !reflect.DeepEqual(sys.events, want) {
		t.Errorf("events %v, want %v", sys.events, want)
	}
	for _, want := range []string{"the idle leg failed", "the loaded leg failed"} {
		t.Run(want, func(t *testing.T) {
			if !strings.Contains(errOut, want) {
				t.Errorf("stderr lacks %q:\n%s", want, errOut)
			}
		})
	}
	if strings.Contains(errOut, "the single leg failed") {
		t.Errorf("a passing leg is reported as failed:\n%s", errOut)
	}
}

func TestACanceledContextStopsBeforeTheNextLeg(t *testing.T) {
	sys := &fakeSystem{cpu: 1}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var o, e bytes.Buffer
	if code := run(ctx, nil, &o, &e, sys); code != exitFailure {
		t.Errorf("code %d, want %d", code, exitFailure)
	}
	if len(sys.events) != 0 {
		t.Errorf("a canceled run started %v", sys.events)
	}
	if !strings.Contains(e.String(), "interrupted before the idle leg") {
		t.Errorf("stderr: %q", e.String())
	}
}

func TestTheCommandLineSelectsLegsCountTestsAndPackages(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantLegs   []string
		wantArgs   []string
		wantLoaded int
	}{
		{name: "one leg", args: []string{"single"}, wantLegs: []string{"single"}, wantArgs: []string{"test", "-race", "-shuffle=on", "-count=3", "./..."}},
		{name: "count, test filter and package", args: []string{"-count", "200", "-run", "^TestA$", "loaded", "--", "./escptest"},
			wantLegs: []string{"loaded"}, wantArgs: []string{"test", "-race", "-shuffle=on", "-count=200", "-run", "^TestA$", "./escptest"}, wantLoaded: 7},
		{name: "legs in the order given", args: []string{"loaded", "idle"}, wantLegs: []string{"loaded", "idle"}, wantArgs: []string{"test", "-race", "-shuffle=on", "-count=3", "./..."}, wantLoaded: 7},
		{name: "hogs", args: []string{"-hogs", "2", "loaded"}, wantLegs: []string{"loaded"}, wantArgs: []string{"test", "-race", "-shuffle=on", "-count=3", "./..."}, wantLoaded: 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts, err := parse(tc.args, 7, io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, l := range opts.legs {
				names = append(names, l.name)
			}
			if !reflect.DeepEqual(names, tc.wantLegs) {
				t.Errorf("legs %v, want %v", names, tc.wantLegs)
			}
			if got := opts.testArgs(); !reflect.DeepEqual(got, tc.wantArgs) {
				t.Errorf("go %v, want go %v", got, tc.wantArgs)
			}
			if tc.wantLoaded != 0 && opts.hogs != tc.wantLoaded {
				t.Errorf("hogs %d, want %d", opts.hogs, tc.wantLoaded)
			}
		})
	}
}

func TestABadCommandLineIsRefusedBeforeAnythingRuns(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown leg", args: []string{"heavy"}, want: `unknown leg "heavy"`},
		{name: "zero count", args: []string{"-count", "0"}, want: "must be positive"},
		{name: "zero hogs", args: []string{"-hogs", "0"}, want: "must be positive"},
		{name: "unknown flag", args: []string{"-nope"}, want: "flag provided but not defined"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sys := &fakeSystem{cpu: 2}
			code, out, errOut := runWith(t, sys, tc.args...)
			if code != exitUsage {
				t.Errorf("code %d, want %d", code, exitUsage)
			}
			if len(sys.events) != 0 || out != "" {
				t.Errorf("something ran: events %v, stdout %q", sys.events, out)
			}
			if !strings.Contains(errOut, tc.want) {
				t.Errorf("stderr lacks %q:\n%s", tc.want, errOut)
			}
		})
	}
}

func TestHelpPrintsTheUsageAndIsNotAFailure(t *testing.T) {
	sys := &fakeSystem{cpu: 2}
	code, out, errOut := runWith(t, sys, "-h")
	if code != exitOK || out != "" || len(sys.events) != 0 {
		t.Errorf("code %d, stdout %q, events %v", code, out, sys.events)
	}
	for _, want := range []string{"usage: soak", "-count", "-hogs"} {
		t.Run(want, func(t *testing.T) {
			if !strings.Contains(errOut, want) {
				t.Errorf("usage lacks %q:\n%s", want, errOut)
			}
		})
	}
	if strings.Contains(errOut, "soak:") {
		t.Errorf("a request for help is reported as an error:\n%s", errOut)
	}
}

func TestSpinRunsUntilItIsToldToStop(t *testing.T) {
	var stop atomic.Bool
	running, ended := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(ended)
		spin(&stop, func() { close(running) })
	}()
	<-running
	select {
	case <-ended:
		t.Fatal("spin returned before it was told to stop")
	default: // spin is still running: it has reported, and nothing has stopped it
	}
	stop.Store(true)
	<-ended
}

func TestTheLoadStartsEveryGoroutineBeforeItReturnsAndStopsThemAllBeforeStopReturns(t *testing.T) {
	const n = 5
	var running, returned atomic.Int32
	stop := startLoad(n, func(stop *atomic.Bool, report func()) {
		running.Add(1)
		report()
		for !stop.Load() {
			runtime.Gosched()
		}
		returned.Add(1)
	})
	if got := running.Load(); got != n {
		t.Errorf("%d goroutines had started when startLoad returned, want %d", got, n)
	}
	if got := returned.Load(); got != 0 {
		t.Errorf("%d goroutines had returned before stop", got)
	}
	stop()
	if got := returned.Load(); got != n {
		t.Errorf("%d goroutines had returned when stop returned, want %d", got, n)
	}
}

func TestTheMachineLoadGivesTheRuntimeAProcessorForEachGoroutineAndOneMore(t *testing.T) {
	prev := runtime.GOMAXPROCS(0)
	t.Cleanup(func() { runtime.GOMAXPROCS(prev) })
	stop := machine{}.load(3)
	defer stop()
	if got := runtime.GOMAXPROCS(0); got != 4 {
		t.Errorf("GOMAXPROCS is %d while loaded, want 4", got)
	}
}

func TestTheMachineRunsGoWithTheLegEnvironmentOverridingTheCallers(t *testing.T) {
	t.Setenv("GOFLAGS", "-mod=vendor")
	var out bytes.Buffer
	if err := (machine{command: exec.CommandContext}).goTest(context.Background(), []string{"GOFLAGS=-mod=mod"}, []string{"env", "GOFLAGS"}, &out); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "-mod=mod" {
		t.Errorf("go saw GOFLAGS=%q, want the leg's -mod=mod", got)
	}
}

func TestTheMachineReportsAFailingGoRun(t *testing.T) {
	var out bytes.Buffer
	if err := (machine{command: exec.CommandContext}).goTest(context.Background(), nil, []string{"no-such-subcommand"}, &out); err == nil {
		t.Error("a go run that exits non-zero returned no error")
	}
	if out.Len() == 0 {
		t.Error("the output of the failed run was not passed on")
	}
}

func TestTheMachineKnowsItsCPUsAndTheTime(t *testing.T) {
	m := machine{}
	if m.cpus() < 1 {
		t.Errorf("cpus %d", m.cpus())
	}
	if m.now().IsZero() {
		t.Error("now is the zero time")
	}
}
