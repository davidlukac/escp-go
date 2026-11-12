package main

import (
	"fmt"
	"io"
	"sort"
)

// writeExtended prints the job summary of a run of the extended operators (`make mutation-extended`):
// the count per outcome, the number of survivors and the efficacy, the survivors per file and then
// each survivor. The run is informational, so the summary says that it has no threshold. A report
// that cannot be read is said in the summary and is not an error, as for the other summaries.
func writeExtended(path string, stdout io.Writer) int {
	_, _ = fmt.Fprintln(stdout, "### Mutation testing, extended operators (informational, no threshold)")
	_, _ = fmt.Fprintln(stdout)
	r, ok := loadReport(path, stdout)
	if !ok {
		return exitOK
	}
	counts, survivors := summarize(r)
	_, _ = fmt.Fprintln(stdout, counts)
	killed, lived := tally(r)
	_, _ = fmt.Fprintln(stdout)
	_, _ = fmt.Fprintf(stdout, "**Survivors: %d.** Efficacy: %s (killed %d of the %d that were run and decided).\n",
		lived, efficacyOf(killed, lived), killed, killed+lived)
	if len(survivors) == 0 {
		return exitOK
	}
	_, _ = fmt.Fprintln(stdout)
	_, _ = fmt.Fprintln(stdout, "Survivors per file:")
	_, _ = fmt.Fprintln(stdout)
	for _, line := range survivorsPerFile(r) {
		_, _ = fmt.Fprintln(stdout, line)
	}
	_, _ = fmt.Fprintln(stdout)
	_, _ = fmt.Fprintln(stdout, "Surviving mutants (a behavior no test pins; an equivalent mutant is not a reason to bend a test):")
	_, _ = fmt.Fprintln(stdout)
	for _, s := range survivors {
		_, _ = fmt.Fprintln(stdout, s)
	}
	return exitOK
}

// tally counts the mutants that were killed and those that lived: the two outcomes gremlins counts
// for its efficacy (timed out, not covered and skipped mutants are not counted).
func tally(r report) (killed, lived int) {
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
	return killed, lived
}

// efficacyOf is killed over killed and lived as a percentage with one decimal, "n/a" when no mutant
// was both run and decided.
func efficacyOf(killed, lived int) string {
	if killed+lived == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(killed)/float64(killed+lived))
}

// survivorsPerFile lists "- `file`: n" for each file with a survivor, most survivors first, ties in
// file order.
func survivorsPerFile(r report) []string {
	type entry struct {
		name string
		n    int
	}
	var entries []entry
	for _, f := range r.Files {
		n := 0
		for _, m := range f.Mutations {
			if m.Status == statusLived {
				n++
			}
		}
		if n > 0 {
			entries = append(entries, entry{f.Name, n})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].n != entries[j].n {
			return entries[i].n > entries[j].n
		}
		return entries[i].name < entries[j].name
	})
	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = fmt.Sprintf("- `%s`: %d", e.name, e.n)
	}
	return lines
}
