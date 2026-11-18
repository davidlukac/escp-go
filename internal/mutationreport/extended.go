package main

import (
	"fmt"
	"io"
	"sort"
)

// writeExtended prints the job summary of a run of the extended operators (`make mutation-extended`):
// the count per outcome, the number of survivors, the efficacy and the timed-out count, the survivors
// per file and then each timed-out mutant and each survivor. The run is informational, so the summary
// says that it has no threshold. A report that cannot be read is said in the summary and is not an
// error, as for the other summaries.
func writeExtended(path string, stdout io.Writer) int {
	_, _ = fmt.Fprintln(stdout, "### Mutation testing, extended operators (informational, no threshold)")
	_, _ = fmt.Fprintln(stdout)
	r, ok := loadReport(path, stdout)
	if !ok {
		return exitOK
	}
	s := summarize(r)
	writeFigures(stdout, s)
	if len(s.survivors) > 0 {
		_, _ = fmt.Fprintln(stdout)
		_, _ = fmt.Fprintln(stdout, "Survivors per file:")
		_, _ = fmt.Fprintln(stdout)
		for _, line := range survivorsPerFile(r) {
			_, _ = fmt.Fprintln(stdout, line)
		}
	}
	writeMutants(stdout, s)
	return exitOK
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
