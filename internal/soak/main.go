// Command soak runs the test suite the way the weekly workflow does: shuffled and repeated with the
// race detector, in three conditions an ordinary run does not have. It is the one source of those
// runs: `make soak` and the steps of .github/workflows/weekly.yml both call it, so that what CI
// runs and what a developer reproduces cannot drift apart. It is a build tool of this repository, not
// part of the API.
//
//	go run ./internal/soak [-count N] [-run REGEXP] [-hogs N] [LEG ...] [-- PACKAGE ...]
//
// The legs are
//
//	idle    the suite on a machine with nothing else to do (the first soak, kept as it was);
//	single  the suite with GOMAXPROCS=1, which makes the scheduler interleave goroutines in orders a
//	        multi-core run seldom produces;
//	loaded  the suite while busy goroutines occupy every CPU, which delays a goroutine for longer than
//	        a short timeout (the stale-datagram defect of the drain showed only there).
//
// With no leg it runs all three, in that order, and with no package it runs ./... . The busy
// goroutines belong to this process, so they stop when it ends, however it ends: there is nothing
// to clean up after a failed or interrupted run. They work on every operating system.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Exit codes of the command.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// Defaults of the flags.
const (
	// defaultCount is how often each test runs in a leg (the weekly soak always ran it three times).
	defaultCount = 3
	// soakCycles is the number of cycles of the leak test (ESCP_SOAK, see leak_test.go); a pull
	// request's run does 20.
	soakCycles = "200"
	// allPackages is what a run without a package list tests.
	allPackages = "./..."
	// packagesMarker separates the legs from the packages on the command line.
	packagesMarker = "--"
)

// leg is one condition the suite is run under.
type leg struct {
	name string
	// env is added to the environment of the test run (and only of it: the busy goroutines of a
	// loaded leg need every CPU of this process).
	env []string
	// loaded says that busy goroutines run beside the tests.
	loaded bool
}

// legs are the conditions, in the order they run.
var legs = []leg{
	{name: "idle", env: []string{"ESCP_SOAK=" + soakCycles}},
	{name: "single", env: []string{"ESCP_SOAK=" + soakCycles, "GOMAXPROCS=1"}},
	{name: "loaded", env: []string{"ESCP_SOAK=" + soakCycles}, loaded: true},
}

// system is what the command needs from the machine; tests replace it.
type system interface {
	// goTest runs `go test args...` with env added to the environment, writing its output to out.
	goTest(ctx context.Context, env, args []string, out io.Writer) error
	// load starts n busy goroutines and returns once all of them run; the returned function stops
	// them and returns once all of them have ended.
	load(n int) (stop func())
	// cpus is the number of CPUs of the machine.
	cpus() int
	// now is the time, for the length of a leg.
	now() time.Time
}

// main is the process entry point.
//
// coverage: ignore - process entry point; run holds all the logic and is tested.
func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, machine{command: exec.CommandContext})
	cancel()
	os.Exit(code)
}

// options is a parsed command line.
type options struct {
	count    int
	run      string
	hogs     int
	legs     []leg
	packages []string
}

// parse reads the command line (without the program name); hogs is the default number of busy goroutines.
func parse(args []string, hogs int, stderr io.Writer) (options, error) {
	opts := options{count: defaultCount, hogs: hogs}
	fs := flag.NewFlagSet("soak", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "usage: soak [flags] [idle|single|loaded ...] [-- package ...]")
		fs.PrintDefaults()
	}
	fs.IntVar(&opts.count, "count", opts.count, "how often each test runs in a leg")
	fs.StringVar(&opts.run, "run", "", "only the tests whose name matches this regular expression")
	fs.IntVar(&opts.hogs, "hogs", opts.hogs, "busy goroutines of a loaded leg (the default is one per CPU)")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if opts.count < 1 || opts.hogs < 1 {
		return options{}, fmt.Errorf("-count and -hogs must be positive, got %d and %d", opts.count, opts.hogs)
	}
	names, packages := fs.Args(), []string(nil)
	if i := slices.Index(names, packagesMarker); i >= 0 {
		names, packages = names[:i], names[i+1:]
	}
	if len(packages) == 0 {
		packages = []string{allPackages}
	}
	opts.packages = packages
	for _, name := range names {
		i := slices.IndexFunc(legs, func(l leg) bool { return l.name == name })
		if i < 0 {
			return options{}, fmt.Errorf("unknown leg %q (the legs are idle, single and loaded)", name)
		}
		opts.legs = append(opts.legs, legs[i])
	}
	if len(opts.legs) == 0 {
		opts.legs = legs
	}
	return opts, nil
}

// testArgs is the command line of `go test` for one leg.
func (o options) testArgs() []string {
	args := []string{"test", "-race", "-shuffle=on", "-count=" + strconv.Itoa(o.count)}
	if o.run != "" {
		args = append(args, "-run", o.run)
	}
	return append(args, o.packages...)
}

// run is the whole command: args are the command line without the program name. Every leg runs even
// when an earlier one failed, because the legs fail for different reasons, and the exit status is
// that of the worst.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, sys system) int {
	opts, err := parse(args, sys.cpus(), stderr)
	if errors.Is(err, flag.ErrHelp) { // the usage is printed already
		return exitOK
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "soak: %v\n", err)
		return exitUsage
	}
	status := exitOK
	for _, l := range opts.legs {
		if ctx.Err() != nil {
			_, _ = fmt.Fprintf(stderr, "soak: interrupted before the %s leg\n", l.name)
			return exitFailure
		}
		if err := runLeg(ctx, l, opts, stdout, sys); err != nil {
			_, _ = fmt.Fprintf(stderr, "soak: the %s leg failed: %v\n", l.name, err)
			status = exitFailure
		}
	}
	return status
}

// runLeg runs the suite once under the condition of l and says how long it took.
func runLeg(ctx context.Context, l leg, opts options, stdout io.Writer, sys system) error {
	_, _ = fmt.Fprintf(stdout, "== soak %s: go %v\n", l.name, opts.testArgs())
	if l.loaded {
		_, _ = fmt.Fprintf(stdout, "== %d busy goroutines beside the tests\n", opts.hogs)
		defer sys.load(opts.hogs)()
	}
	began := sys.now()
	err := sys.goTest(ctx, l.env, opts.testArgs(), stdout)
	_, _ = fmt.Fprintf(stdout, "== soak %s took %s\n", l.name, sys.now().Sub(began).Round(time.Second))
	return err
}

// machine is this computer; command builds the processes it starts (a test could replace it).
type machine struct {
	command func(ctx context.Context, name string, args ...string) *exec.Cmd
}

func (machine) cpus() int      { return runtime.NumCPU() }
func (machine) now() time.Time { return time.Now() }

func (m machine) goTest(ctx context.Context, env, args []string, out io.Writer) error {
	cmd := m.command(ctx, "go", args...)
	cmd.Env = append(os.Environ(), env...) // the last value of a name wins
	cmd.Stdout, cmd.Stderr = out, out
	return cmd.Run()
}

// load starts the busy goroutines. The runtime gets a processor for each of them whatever
// GOMAXPROCS says in the environment this command was started in; the tests get their own
// environment (see leg.env).
func (machine) load(n int) func() {
	runtime.GOMAXPROCS(n + 1) // one more, so that this goroutine and the output copy are not starved
	return startLoad(n, spin)
}

// startLoad runs work in n goroutines and returns once all of them have called running; the
// returned function sets their stop flag and returns once all of them have returned.
func startLoad(n int, work func(stop *atomic.Bool, running func())) func() {
	var (
		stop    atomic.Bool
		started sync.WaitGroup
		ended   sync.WaitGroup
	)
	started.Add(n)
	ended.Add(n)
	for range n {
		go func() {
			defer ended.Done()
			work(&stop, started.Done)
		}()
	}
	started.Wait()
	return func() {
		stop.Store(true)
		ended.Wait()
	}
}

// spin burns a processor until stop is set; running is called once, when it has begun.
func spin(stop *atomic.Bool, running func()) {
	running()
	for !stop.Load() {
	}
}
