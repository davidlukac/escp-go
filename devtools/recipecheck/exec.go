package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// scrubbed are the variables of the environment that a run of make must not inherit: the ones the
// Makefile reads (the case sets what it needs), and the ones an outer make passes on.
var scrubbed = []string{
	"MAKEFLAGS", "MFLAGS", "MAKELEVEL",
	"EFFICACY", "TIMEOUT_COEFFICIENT", "WORKERS", "PKG", "BASE", "MUTATE", "MUTATE_EXTENDED", "GREMLINS",
	"STANDIN_LOG", "STANDIN_REPORT",
}

// execRunner runs make for real, as a fresh one: --no-print-directory, and an environment without the
// variables in scrubbed, plus the case's own.
func execRunner(root, makeBin string) runner {
	return func(ctx context.Context, env []string, args ...string) (string, int, error) {
		cmd := exec.CommandContext(ctx, makeBin, append([]string{"--no-print-directory"}, args...)...) //nolint:gosec // the make the operator names
		cmd.Dir = root
		cmd.Env = append(scrubEnv(os.Environ()), env...)
		var out bytes.Buffer
		cmd.Stdout = &out
		err := cmd.Run()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", 0, ctxErr
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return out.String(), exitErr.ExitCode(), nil
		}
		return out.String(), 0, err
	}
}

// scrubEnv is the environment without the variables in scrubbed.
func scrubEnv(environ []string) []string {
	kept := make([]string, 0, len(environ))
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if !slices.Contains(scrubbed, name) {
			kept = append(kept, entry)
		}
	}
	return kept
}
