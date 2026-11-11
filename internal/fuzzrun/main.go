// Command fuzzrun runs every fuzz target of the module for a number of seconds each (default 30).
// The seed corpora (testdata/fuzz) already run as ordinary tests in `make test`; this explores
// beyond them. A failure leaves its input under the package's testdata/fuzz: commit it with the fix.
// It is a build tool of this repository, not part of the API.
//
//	go run ./internal/fuzzrun [SECONDS]
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Exit codes of the command.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// defaultSeconds is how long each target is fuzzed when the command line names no number.
const defaultSeconds = 30

// fuzzPrefix starts the name of every fuzz target (the go tool's rule).
const fuzzPrefix = "Fuzz"

// goTool is what the command needs from the go tool; tests replace it.
type goTool interface {
	// packages lists the import paths of the module.
	packages(ctx context.Context) ([]string, error)
	// targets lists the names of the fuzz targets of one package.
	targets(ctx context.Context, pkg string) ([]string, error)
	// fuzz runs one target for d, writing the tool's output to out.
	fuzz(ctx context.Context, pkg, target string, d time.Duration, out io.Writer) error
}

// main is the process entry point.
//
// coverage: ignore - process entry point; run holds all the logic and is tested.
func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, execTool{command: exec.CommandContext}))
}

// run is the whole command: args are the command line without the program name.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, tool goTool) int {
	secs := defaultSeconds
	switch len(args) {
	case 0:
	case 1:
		n, err := strconv.Atoi(args[0])
		if err != nil || n < 1 {
			_, _ = fmt.Fprintf(stderr, "fuzzrun: %q is not a number of seconds\n", args[0])
			return exitUsage
		}
		secs = n
	default:
		_, _ = fmt.Fprintln(stderr, "usage: fuzzrun [SECONDS]")
		return exitUsage
	}
	pkgs, err := tool.packages(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "fuzzrun: listing the packages: %v\n", err)
		return exitFailure
	}
	status := exitOK
	for _, pkg := range pkgs {
		targets, err := tool.targets(ctx, pkg)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "fuzzrun: listing the tests of %s: %v\n", pkg, err)
			status = exitFailure
			continue
		}
		for _, target := range targets {
			_, _ = fmt.Fprintf(stdout, "== %s %s (%ds)\n", pkg, target, secs)
			if err := tool.fuzz(ctx, pkg, target, time.Duration(secs)*time.Second, stdout); err != nil {
				_, _ = fmt.Fprintf(stderr, "fuzzrun: %s %s failed: %v\n", pkg, target, err)
				status = exitFailure
			}
		}
	}
	return status
}

// fuzzTargets picks the fuzz targets out of the output of `go test -list`.
func fuzzTargets(listing string) []string {
	var out []string
	for line := range strings.SplitSeq(listing, "\n") {
		if name := strings.TrimSpace(line); strings.HasPrefix(name, fuzzPrefix) {
			out = append(out, name)
		}
	}
	return out
}

// execTool is the go tool of this machine; command builds the processes it starts (tests replace it).
type execTool struct {
	command func(ctx context.Context, name string, args ...string) *exec.Cmd
}

func (t execTool) packages(ctx context.Context) ([]string, error) {
	out, err := t.command(ctx, "go", "list", "./...").Output()
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

func (t execTool) targets(ctx context.Context, pkg string) ([]string, error) {
	out, err := t.command(ctx, "go", "test", "-list", "^"+fuzzPrefix, pkg).Output()
	if err != nil {
		return nil, err
	}
	return fuzzTargets(string(out)), nil
}

func (t execTool) fuzz(ctx context.Context, pkg, target string, d time.Duration, out io.Writer) error {
	cmd := t.command(ctx, "go", "test", pkg, "-run", "^$", "-fuzz", "^"+target+"$", "-fuzztime", d.String())
	cmd.Stdout, cmd.Stderr = out, out
	return cmd.Run()
}
