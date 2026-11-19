package main

import (
	"fmt"
	"io"
	"os"
)

// standinCommand is the argument that turns this program into the stand-in for gremlins: the recipes run
// it where they run the mutation tool (MUTATE, MUTATE_EXTENDED), with gremlins' own arguments, which it
// ignores.
const standinCommand = "standin"

// runStandin does what a finished gremlins run does for the check: it leaves a mark in the file named by
// STANDIN_LOG that it ran, and copies the report named by STANDIN_REPORT, when there is one, to
// build/mutation.json, where gremlins writes its own. It runs in the directory of the repository.
func runStandin(getenv func(string) string, stderr io.Writer) int {
	if log := getenv("STANDIN_LOG"); log != "" {
		f, err := os.OpenFile(log, os.O_APPEND|os.O_WRONLY|os.O_CREATE, fileMode) //nolint:gosec // the log the check names
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "recipecheck standin: %v\n", err)
			return exitError
		}
		_, _ = f.WriteString("ran\n")
		_ = f.Close()
	}
	src := getenv("STANDIN_REPORT")
	if src == "" {
		return exitOK
	}
	data, err := os.ReadFile(src) //nolint:gosec // the report the check names
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "recipecheck standin: %v\n", err)
		return exitError
	}
	if err := os.WriteFile(reportFile, data, fileMode); err != nil { //nolint:gosec // the constant path build/mutation.json
		_, _ = fmt.Fprintf(stderr, "recipecheck standin: %v\n", err)
		return exitError
	}
	return exitOK
}
