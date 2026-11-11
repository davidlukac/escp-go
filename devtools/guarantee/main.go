// Command guarantee lists the exported identifiers whose doc comment states a guarantee ("sends
// nothing", "never", "at most once", "safe for concurrent use", ...) and that no test names.
//
//	go -C devtools run ./guarantee -root .
//
// A sentence in a doc comment that promises a behavior is only as good as the test that fails when
// the sentence is false (defect class DC-C01 of docs/defect-classes.md). A test says which promise it
// pins with a marker comment, anywhere in a _test.go file of the package that declares the identifier:
//
//	// guarantee: Client.Close
//
// The name is the identifier (`Dial`, `ErrClosed`), a method or a field as `Type.Name`; several names
// may follow one marker, separated by commas or spaces.
//
// The command is in report mode: it prints one line per unmatched identifier (`file:line: Name:
// sentence`), the names a marker uses that no exported identifier has, and the count, and exits 0.
// With -strict it exits 1 when there is anything to report; that is for when the existing tests carry
// their markers. It decides from the text of the comments only, with patterns for the usual wording of
// a promise, so it misses a promise worded differently and flags a sentence that is not one; deciding
// what is a guarantee needs a person, the check only keeps the list from growing silently.
//
// It reads Go source with go/parser and has no dependency outside the standard library.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// main is the process entry point.
func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run parses the flags, scans the repository and prints the report; it returns the exit status: 0
// (also when there is something to report, unless -strict), 1 for a strict report with findings, 2
// for a usage error or an unreadable repository.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("guarantee", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "the repository root")
	strict := fs.Bool("strict", false, "exit 1 when an identifier lacks its marker or a marker names nothing")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	res, err := Scan(*root)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "guarantee:", err)
		return 2
	}
	res.Print(stdout)
	if *strict && res.HasFindings() {
		return 1
	}
	return 0
}
