// Command recipecheck pins the recipes of `make mutation`, `make mutation-diff` and `make mutation-extended`.
// It runs `make -n` for each target with the variables the jobs use and compares the recipe it prints
// with what it must be, line by line and whole (a substring cannot satisfy it), then runs make with a
// stand-in for gremlins and checks the exit codes, because `make -n` hides a `-` prefix, a failing
// step that does not fail make.
//
//	go -C devtools run ./recipecheck -root .
//
// What it pins:
//
//   - the old report is deleted first (gremlins exits 0 and writes no report when it has nothing to
//     report, so a stale one would be judged in place of the run);
//   - with EFFICACY set to anything, an explicitly empty value included, the threshold is validated
//     first and the efficacy check runs after gremlins, with the value given (two values are tried, so a
//     hardcoded one fails); without it no check runs (the origin of the variable counts, not its value);
//     the extended run never has either;
//   - the timeout coefficient is validated before gremlins in every target, with the value given, from
//     the command line and from the environment;
//   - the line of gremlins starts with `GOFLAGS=-count=1` (a cached baseline makes every mutant time
//     out, and a timeout counts as killed), hands gremlins the coefficient, --workers and the PKG filter
//     and the report path the check reads, and never `--threshold-efficacy`;
//   - the exit codes: a good, a bad, a missing and a stale report, a threshold or a coefficient that must
//     be refused before gremlins starts;
//   - `make lint` runs this check, and the dead-code check (`make deadcode`): both of its recipe lines
//     are printed whole by `make -n lint`, and as written (no `-` or `@` prefix) in the Makefile.
//
// The report of the working tree (build/mutation.json) is put aside while the exit codes are run and put
// back with its permissions, on an interrupt (INT, TERM, HUP) too. Each make runs as a fresh one, with
// --no-print-directory and without MAKEFLAGS, MFLAGS, MAKELEVEL and the variables the Makefile reads
// (GNU make 4 prints "Entering directory" lines when it runs inside another make, and the command-line
// variables of the outer one travel in MAKEFLAGS).
//
// The program is also the stand-in for gremlins: `recipecheck standin` (see runStandin).
//
// It exits 0 when every recipe is as it must be, 1 when one is not (one message per problem on stderr)
// and 2 for a usage error or a make that could not run.
package main

import (
	"cmp"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

// Exit statuses.
const (
	exitOK       = 0
	exitProblems = 1
	exitError    = 2
)

// interrupts are the signals that stop the check: it restores the report of the working tree and exits
// (a wrapper such as `timeout` sends TERM to this process alone).
var interrupts = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

// listen is the context that is canceled by one of the interrupts.
func listen(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, interrupts...)
}

// main is the process entry point.
func main() {
	ctx, stop := listen(context.Background())
	code := run(ctx, os.Args[1:], processEnvironment())
	stop()
	os.Exit(code)
}

// environment is what the command takes from the process, so that the tests can replace it.
type environment struct {
	stdout, stderr io.Writer
	getenv         func(string) string
	executable     func() (string, error)
	abs            func(string) (string, error)
	newRunner      func(root, makeBin string) runner
	files          files
}

// processEnvironment is the real one.
func processEnvironment() environment {
	return environment{stdout: os.Stdout, stderr: os.Stderr, getenv: os.Getenv, executable: os.Executable,
		abs: filepath.Abs, newRunner: execRunner, files: osFiles{}}
}

// run is the whole command: args are the command line without the program name.
func run(ctx context.Context, args []string, e environment) int {
	if len(args) > 0 && args[0] == standinCommand {
		return runStandin(e.getenv, e.stderr)
	}
	fs := flag.NewFlagSet("recipecheck", flag.ContinueOnError)
	fs.SetOutput(e.stderr)
	root := fs.String("root", ".", "the root of the repository (where the Makefile is)")
	makeBin := fs.String("make", "", "the make program (default: $MAKE, or make)")
	if err := fs.Parse(args); err != nil {
		return exitError
	}
	if *makeBin == "" {
		*makeBin = cmp.Or(e.getenv("MAKE"), "make")
	}
	exe, err := e.executable()
	if err != nil {
		_, _ = fmt.Fprintf(e.stderr, "recipecheck: %v\n", err)
		return exitError
	}
	absRoot, err := e.abs(*root)
	if err != nil {
		_, _ = fmt.Fprintf(e.stderr, "recipecheck: %v\n", err)
		return exitError
	}
	c := &checker{root: absRoot, run: e.newRunner(absRoot, *makeBin), files: e.files,
		standin: shellQuote(exe) + " " + standinCommand}
	problems, err := c.check(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(e.stderr, "recipecheck: %v\n", err)
		return exitError
	}
	for _, p := range problems {
		_, _ = fmt.Fprintf(e.stderr, "recipecheck: %s\n", p)
	}
	if len(problems) > 0 {
		return exitProblems
	}
	_, _ = fmt.Fprintln(e.stdout, "recipecheck: the mutation recipes are as pinned")
	return exitOK
}

// shellQuote quotes a word for the shell make runs its recipes in.
func shellQuote(word string) string {
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}
