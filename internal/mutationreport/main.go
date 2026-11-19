// Command mutationreport turns the JSON report of a mutation run (gremlins, `make mutation`) into
// the Markdown of a job summary: the count per outcome, the efficacy, the mutants that timed out and
// those that survived. With `badge` it prints a shields.io endpoint file with the efficacy instead,
// with `check` it enforces a threshold on it (the check passes AT the threshold; gremlins' own gate
// failed at equality), and `threshold` only validates a threshold. The efficacy is (killed + timed
// out) over (killed + timed out + lived): a mutant that makes the tests hang is noticed by the test
// run, which is a kill as PIT and Stryker count it, whereas gremlins leaves it out of its own figure.
// It is a build tool of this repository, not part of the API.
//
//	go run ./internal/mutationreport build/mutation.json >> "$GITHUB_STEP_SUMMARY"
//	go run ./internal/mutationreport badge build/mutation.json > mutation.json
//	go run ./internal/mutationreport check build/mutation.json 90
//	go run ./internal/mutationreport threshold 90
//	go run ./internal/mutationreport coefficient 10
//	go run ./internal/mutationreport extended build/mutation-extended.json >> "$GITHUB_STEP_SUMMARY"
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Exit codes of the command.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// Outcomes of a mutant, as gremlins spells them.
const (
	statusLived    = "LIVED"
	statusKilled   = "KILLED"
	statusTimedOut = "TIMED OUT"
)

// report is the part of gremlins' JSON report this command reads.
type report struct {
	Files []struct {
		Name      string `json:"file_name"`
		Mutations []struct {
			Type   string `json:"type"`
			Status string `json:"status"`
			Line   int    `json:"line"`
			Column int    `json:"column"`
		} `json:"mutations"`
	} `json:"files"`
}

// main is the process entry point.
//
// coverage: ignore - process entry point; run holds all the logic and is tested.
func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// usage is what the command prints for a command line it does not understand.
const usage = "usage: mutationreport REPORT.json | mutationreport badge REPORT.json | " +
	"mutationreport check REPORT.json THRESHOLD | mutationreport threshold THRESHOLD | mutationreport coefficient N | mutationreport extended REPORT.json"

// run is the whole command: args are the command line without the program name.
func run(args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 2 && args[0] == "badge":
		return writeBadge(args[1], stdout, stderr)
	case len(args) == 2 && args[0] == "extended":
		return writeExtended(args[1], stdout)
	case len(args) == 2 && args[0] == "threshold":
		return writeThreshold(args[1], stderr)
	case len(args) == 2 && args[0] == "coefficient":
		return writeCoefficient(args[1], stderr)
	case len(args) == 3 && args[0] == "check":
		return writeCheck(args[1], args[2], stdout, stderr)
	case len(args) == 1 && !isCommand(args[0]):
		return writeSummary(args[0], stdout)
	}
	_, _ = fmt.Fprintln(stderr, usage)
	return exitUsage
}

// isCommand says whether word is the name of a subcommand: a lone one is a command line missing its
// arguments, not the path of a report.
func isCommand(word string) bool {
	return word == "badge" || word == "check" || word == "extended" || word == "threshold" || word == "coefficient"
}

// writeSummary prints the job summary of a run of `make mutation`. A report that cannot be read is
// said in the summary and is not an error: a summary must never fail the job.
func writeSummary(path string, stdout io.Writer) int {
	_, _ = fmt.Fprintln(stdout, "### Mutation testing")
	_, _ = fmt.Fprintln(stdout)
	r, ok := loadReport(path, stdout)
	if !ok {
		return exitOK
	}
	s := summarize(r)
	writeFigures(stdout, s)
	writeMutants(stdout, s)
	return exitOK
}

// errNoReport is what readReport returns, wrapped, when the file cannot be read at all.
var errNoReport = errors.New("no report")

// readReport reads and parses the report at path, the one place that does. A file that cannot be read
// is an error that wraps errNoReport; a file that is not a report is the parser's own error.
func readReport(path string) (r report, err error) {
	text, err := os.ReadFile(path) //nolint:gosec // the report the operator names on the command line
	if err != nil {
		return r, fmt.Errorf("%w: %w", errNoReport, err)
	}
	if err := json.Unmarshal(text, &r); err != nil {
		return report{}, err
	}
	return r, nil
}

// loadReport reads the report at path. When it cannot (the run stopped before it wrote one, or wrote
// something else), it says so on stdout, for the job summary, and returns false.
func loadReport(path string, stdout io.Writer) (r report, ok bool) {
	r, err := readReport(path)
	switch {
	case errors.Is(err, errNoReport):
		_, _ = fmt.Fprintln(stdout, "No report: the run stopped before it wrote one.")
		return r, false
	case err != nil:
		_, _ = fmt.Fprintf(stdout, "The report cannot be read: %v\n", err)
		return report{}, false
	}
	return r, true
}

// summary is what a report says in words: the count per outcome ("KILLED: 16, LIVED: 1"), the three
// figures of the efficacy, and the mutants that lived and that timed out, one Markdown bullet each, in
// the order of the report.
type summary struct {
	counts    string
	tally     tally
	survivors []string
	timedOut  []string
}

// summarize reads the report into a summary.
func summarize(r report) summary {
	s := summary{tally: tallyOf(r)}
	byStatus := map[string]int{}
	for _, f := range r.Files {
		for _, m := range f.Mutations {
			byStatus[m.Status]++
			line := fmt.Sprintf("- `%s:%d:%d` %s", f.Name, m.Line, m.Column, m.Type)
			switch m.Status {
			case statusLived:
				s.survivors = append(s.survivors, line)
			case statusTimedOut:
				s.timedOut = append(s.timedOut, line)
			}
		}
	}
	if len(byStatus) == 0 {
		s.counts = "No mutants in scope."
		return s
	}
	names := make([]string, 0, len(byStatus))
	for status := range byStatus {
		names = append(names, status)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, status := range names {
		parts[i] = fmt.Sprintf("%s: %d", status, byStatus[status])
	}
	s.counts = strings.Join(parts, ", ")
	return s
}

// writeFigures prints the count per outcome, then the survivors and the efficacy, then the timed-out
// count on a line of its own, so that a rise is seen.
func writeFigures(w io.Writer, s summary) {
	t := s.tally
	_, _ = fmt.Fprintln(w, s.counts)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "**Survivors: %d.** Efficacy: %s (killed %d and timed out %d of the %d that were run and decided).\n",
		t.lived, t.efficacyText(), t.killed, t.timedOut, t.decided())
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, "**Timed out: %d** (counted as killed; an unexplained rise is investigated: a slow test, a cached baseline, a real infinite loop).\n", t.timedOut)
}

// writeMutants lists the mutants that timed out and those that survived, each list under a heading
// and only when it is not empty.
func writeMutants(w io.Writer, s summary) {
	for _, list := range []struct {
		heading string
		lines   []string
	}{
		{"Timed out mutants (counted as killed; the tests did not finish, which is a kill, but check that the cause is the mutant and not a slow test):", s.timedOut},
		{"Surviving mutants (a behavior no test pins; an equivalent mutant is not a reason to bend a test):", s.survivors},
	} {
		if len(list.lines) == 0 {
			continue
		}
		_, _ = fmt.Fprintln(w)
		_, _ = fmt.Fprintln(w, list.heading)
		_, _ = fmt.Fprintln(w)
		for _, line := range list.lines {
			_, _ = fmt.Fprintln(w, line)
		}
	}
}

// badge is a shields.io endpoint file: https://shields.io/badges/endpoint-badge.
type badge struct {
	SchemaVersion int    `json:"schemaVersion"`
	Label         string `json:"label"`
	Message       string `json:"message"`
	Color         string `json:"color"`
}

// colorBands are the lowest efficacy percentages of each color, best first; anything lower is red.
var colorBands = []struct {
	from  float64
	color string
}{{95, "brightgreen"}, {90, "green"}, {80, "yellowgreen"}, {70, "yellow"}, {60, "orange"}}

// writeBadge reads a report and prints its badge. A report that cannot be read is a failure: a badge
// must say what was measured, and the workflow that publishes it leaves the old one in place.
func writeBadge(path string, stdout, stderr io.Writer) int {
	r, err := readReport(path)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "mutationreport: %s\n", describe(err))
		return exitFailure
	}
	if err := json.NewEncoder(stdout).Encode(badgeOf(r)); err != nil {
		_, _ = fmt.Fprintf(stderr, "mutationreport: %v\n", err)
		return exitFailure
	}
	return exitOK
}

// describe says why readReport failed, for a command that exits with an error.
func describe(err error) string {
	if errors.Is(err, errNoReport) {
		return err.Error()
	}
	return "the report cannot be read: " + err.Error()
}

// badgeOf is the efficacy of a report as a badge, "n/a" when no mutant was run and decided.
func badgeOf(r report) badge {
	t := tallyOf(r)
	b := badge{SchemaVersion: 1, Label: "mutation score"}
	pct, ok := t.efficacy()
	if !ok {
		b.Message, b.Color = "n/a", "lightgrey"
		return b
	}
	b.Message, b.Color = t.efficacyText(), "red"
	for _, band := range colorBands {
		if pct >= band.from {
			b.Color = band.color
			break
		}
	}
	return b
}
