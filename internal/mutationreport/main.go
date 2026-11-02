// Command mutationreport turns the JSON report of a mutation run (gremlins, `make mutation`) into
// the Markdown of a job summary: the count per outcome, then the mutants that survived. With `badge`
// it prints a shields.io endpoint file with the efficacy instead (killed over killed and survived, the
// number gremlins reports). It is a build tool of this repository, not part of the API.
//
//	go run ./internal/mutationreport build/mutation.json >> "$GITHUB_STEP_SUMMARY"
//	go run ./internal/mutationreport badge build/mutation.json > mutation.json
package main

import (
	"encoding/json"
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
	statusLived  = "LIVED"
	statusKilled = "KILLED"
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

// run is the whole command: args are the command line without the program name.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 2 && args[0] == "badge" {
		return writeBadge(args[1], stdout, stderr)
	}
	if len(args) != 1 {
		_, _ = fmt.Fprintln(stderr, "usage: mutationreport REPORT.json | mutationreport badge REPORT.json")
		return exitUsage
	}
	_, _ = fmt.Fprintln(stdout, "### Mutation testing")
	_, _ = fmt.Fprintln(stdout)
	text, err := os.ReadFile(args[0]) //nolint:gosec // the report the operator names on the command line
	if err != nil {
		_, _ = fmt.Fprintln(stdout, "No report: the run stopped before it wrote one.")
		return exitOK
	}
	var r report
	if err := json.Unmarshal(text, &r); err != nil {
		_, _ = fmt.Fprintf(stdout, "The report cannot be read: %v\n", err)
		return exitOK
	}
	counts, survivors := summarize(r)
	_, _ = fmt.Fprintln(stdout, counts)
	if len(survivors) > 0 {
		_, _ = fmt.Fprintln(stdout)
		_, _ = fmt.Fprintln(stdout, "Surviving mutants (a behavior no test pins; an equivalent mutant is not a reason to bend a test):")
		_, _ = fmt.Fprintln(stdout)
		for _, s := range survivors {
			_, _ = fmt.Fprintln(stdout, s)
		}
	}
	return exitOK
}

// summarize counts the mutants per outcome ("KILLED: 16, LIVED: 1") and lists the ones that lived,
// one Markdown bullet each, in the order of the report.
func summarize(r report) (counts string, survivors []string) {
	byStatus := map[string]int{}
	for _, f := range r.Files {
		for _, m := range f.Mutations {
			byStatus[m.Status]++
			if m.Status == statusLived {
				survivors = append(survivors, fmt.Sprintf("- `%s:%d:%d` %s", f.Name, m.Line, m.Column, m.Type))
			}
		}
	}
	if len(byStatus) == 0 {
		return "No mutants in scope.", nil
	}
	names := make([]string, 0, len(byStatus))
	for s := range byStatus {
		names = append(names, s)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, s := range names {
		parts[i] = fmt.Sprintf("%s: %d", s, byStatus[s])
	}
	return strings.Join(parts, ", "), survivors
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
	text, err := os.ReadFile(path) //nolint:gosec // the report the operator names on the command line
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "mutationreport: %v\n", err)
		return exitFailure
	}
	var r report
	if err := json.Unmarshal(text, &r); err != nil {
		_, _ = fmt.Fprintf(stderr, "mutationreport: the report cannot be read: %v\n", err)
		return exitFailure
	}
	if err := json.NewEncoder(stdout).Encode(badgeOf(r)); err != nil {
		_, _ = fmt.Fprintf(stderr, "mutationreport: %v\n", err)
		return exitFailure
	}
	return exitOK
}

// badgeOf is the efficacy of a report as a badge: killed mutants over killed and survived ones (timed
// out, not covered and skipped mutants are not counted, as gremlins does not count them), "n/a" when
// no mutant was both run and decided.
func badgeOf(r report) badge {
	killed, lived := 0, 0
	for _, f := range r.Files {
		for _, m := range f.Mutations {
			switch m.Status {
			case statusKilled:
				killed++
			case statusLived:
				lived++
			}
		}
	}
	b := badge{SchemaVersion: 1, Label: "mutation score"}
	if killed+lived == 0 {
		b.Message, b.Color = "n/a", "lightgrey"
		return b
	}
	pct := 100 * float64(killed) / float64(killed+lived)
	b.Message, b.Color = fmt.Sprintf("%.1f%%", pct), "red"
	for _, band := range colorBands {
		if pct >= band.from {
			b.Color = band.color
			break
		}
	}
	return b
}
