// Command catalogcheck verifies that the defect catalog (docs/defect-classes.md) is true: that every
// check it names exists, so that renaming a test or removing a make target cannot leave a sentence
// that is no longer so.
//
//	go -C devtools run ./catalogcheck -root .
//
// It prints one line per problem, `file:line: message`, and exits 1 if there is any. What it checks:
//
//   - the shape of each entry: an anchor and a heading with the same id, then the fields Rule, Why,
//     Design question, Test requirement, Automation and Adversarial probe, in that order, none empty;
//   - that every entry is in the routing table, and that every link of the routing table is an entry;
//   - the tokens of each Automation field (`kind:name`, the kinds are listed in the catalog): a `test:`
//     or `fuzz:` name is a function of a `_test.go` file, a `make:` name a target of the Makefile, a
//     `lint:` name a linter enabled in .golangci.yml, an `analyzer:` name a directory of devtools (or
//     the name of an analyzer declared there), a `script:` name a file, a `workflow:` name a file of
//     .github/workflows; `reading:` carries its reason, `proposed:` the issue that tracks it in
//     parentheses, and a proposal whose name already exists is out of date;
//   - that every `DC-xnn` id written anywhere else in the repository, and every link into the catalog,
//     resolves.
//
// The check is specific to this repository's catalog: the provider has its own catalog and its own
// names. It reads files only, never runs a test or a tool, and has no dependency outside the standard
// library.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// main is the process entry point.
func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run parses the flags, checks the repository and prints the problems; it returns the exit status:
// 0 for a catalog without problems, 1 for problems, 2 for a usage error or an unreadable repository.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("catalogcheck", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "the repository root (the directory that holds docs/defect-classes.md)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	problems, err := Check(*root)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "catalogcheck:", err)
		return 2
	}
	for _, p := range problems {
		_, _ = fmt.Fprintln(stdout, p)
	}
	if len(problems) > 0 {
		_, _ = fmt.Fprintf(stderr, "catalogcheck: %d problem(s) in the defect catalog\n", len(problems))
		return 1
	}
	return 0
}
