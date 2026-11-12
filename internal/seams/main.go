// Command seams lists, for a range of the repository's history, the exported symbols whose behavior
// changed, so that a release reviewer knows which seams to run a consumer against. It is a build tool of
// this repository, not part of the API, and `scripts/seams` runs it.
//
//	scripts/seams [-provider DIR] [-all] FROM TO        for example: scripts/seams v0.9.0 v0.10.0
//
// FROM and TO are tags, branches or commits of the repository the command runs in; FROM must be an
// ancestor of TO, and a TO of HEAD needs a clean working tree (the listing is of committed code). The
// command refuses an unknown name, an empty range and a range that does not run forward.
//
// It reads the Go files of both revisions, leaving out tests, internal packages, tools and examples, and
// reports each exported function, method, type, constant and variable that was
//
//   - added, removed, or changed in its signature (the type of a function or a value, the definition of
//     a type) or in its body, and
//   - unchanged itself but calling, or reading, a declaration that changed, even an unexported one
//     ("via settle"): a fix in a helper reaches every symbol that uses it.
//
// The graph behind the second kind is built by name, without types: a call is matched to the
// declaration of that name in the same package, a method call to every method of that name. It
// over-reports and never under-reports a direct call; it does not see a call through an interface value
// or a function stored in a field. A struct field, an unexported type and a comment are not compared.
//
// With a consumer checkout (-provider DIR, or the environment variable SEAMS_PROVIDER) the list is cut
// to the symbols the consumer uses, with the places where it does: a symbol is found through the import
// of the library; a method is found by its name alone, which is marked "~" because the receiver's type
// is not known. -all lists the other symbols too and every place of use.
//
// The second part lists the changed declarations that no caller can name (unexported functions and
// methods, constants and variables), each with the number of exported symbols that reach it by any path:
// a fix in a helper such as the drain before a request reaches every call that sends one, and the first
// part, which names only the nearest changed declaration, would not say so. The third part is the
// production files that more than one commit of the range touched, with the number of commits: the
// places where fixes of one release may combine.
//
// The command only reads. Exit status 0 is a listing, 1 a failure (named on stderr), 2 a wrong command
// line.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// Exit codes of the command.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

const (
	// providerVariable names the environment variable that holds the consumer checkout.
	providerVariable = "SEAMS_PROVIDER"
	usageLine        = "usage: seams [-provider DIR] [-all] FROM TO"
)

// options is the command line.
type options struct {
	from, to string
	consumer string
	all      bool
}

// main is the process entry point.
//
// coverage: ignore - process entry point; run holds all the logic and is tested.
func main() {
	os.Exit(run(os.Args[1:], os.Getenv, ".", os.Stdout, os.Stderr))
}

// run is the whole command: args are the command line without the program name, dir the repository.
func run(args []string, getenv func(string) string, dir string, stdout, stderr io.Writer) int {
	o, err := parseArgs(args, getenv)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "seams: %v\n%s\n", err, usageLine)
		return exitUsage
	}
	if err := execute(o, systemGit(dir), stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "seams:", err)
		return exitFailure
	}
	return exitOK
}

// parseArgs reads the command line; the consumer defaults to the environment.
func parseArgs(args []string, getenv func(string) string) (options, error) {
	var o options
	fs := flag.NewFlagSet("seams", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.consumer, "provider", getenv(providerVariable), "")
	fs.BoolVar(&o.all, "all", false, "")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() != 2 {
		return options{}, errors.New("two revisions are needed: FROM and TO")
	}
	o.from, o.to = fs.Arg(0), fs.Arg(1)
	for _, ref := range []string{o.from, o.to} {
		if ref == "" || strings.HasPrefix(ref, "-") {
			return options{}, fmt.Errorf("%q is not a revision", ref)
		}
	}
	return o, nil
}

// execute lists the seams of the range o names.
func execute(o options, g gitFunc, stdout io.Writer) error {
	rng, err := resolveRange(g, o.from, o.to)
	if err != nil {
		return err
	}
	from, err := loadTree(g, rng.fromSHA)
	if err != nil {
		return fmt.Errorf("%s: %w", o.from, err)
	}
	to, err := loadTree(g, rng.toSHA)
	if err != nil {
		return fmt.Errorf("%s: %w", o.to, err)
	}
	var u *usage
	if o.consumer != "" {
		if u, err = scanConsumer(o.consumer, to); err != nil {
			return err
		}
	}
	files, err := commitCounts(g, rng)
	if err != nil {
		return err
	}
	return render(stdout, report{
		opts: o, rng: rng, module: to.module, cmp: compare(from, to), use: u, files: files,
	})
}
