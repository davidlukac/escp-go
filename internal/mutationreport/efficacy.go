package main

import (
	"fmt"
	"io"
	"strconv"
)

// tally counts the mutants that decide the efficacy. Not covered and skipped mutants were not run,
// so they are in none of the three.
type tally struct {
	killed, timedOut, lived int
}

// tallyOf counts the report's mutants per deciding outcome.
func tallyOf(r report) tally {
	var t tally
	for _, f := range r.Files {
		for _, m := range f.Mutations {
			switch m.Status {
			case statusKilled:
				t.killed++
			case statusTimedOut:
				t.timedOut++
			case statusLived:
				t.lived++
			}
		}
	}
	return t
}

// decided is the number of mutants that were run and decided.
func (t tally) decided() int { return t.killed + t.timedOut + t.lived }

// efficacy is the one definition the summary, the badge and the check share: the percentage of the
// decided mutants that were caught, (killed + timed out) over (killed + timed out + lived). A mutant
// that makes the tests hang is noticed by the test run, which is a kill by the usual definition
// (PIT and Stryker count it so); gremlins leaves it out of its own figure. ok is false when no mutant
// was decided: there is no efficacy then.
func (t tally) efficacy() (pct float64, ok bool) {
	if t.decided() == 0 {
		return 0, false
	}
	return 100 * float64(t.killed+t.timedOut) / float64(t.decided()), true
}

// efficacyText is the efficacy as a percentage with one decimal, "n/a" when there is none.
func (t tally) efficacyText() string {
	pct, ok := t.efficacy()
	if !ok {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", pct)
}

// writeCheck is the `check` command: it prints one line with the figures of the report and returns
// exitOK when the efficacy reaches the threshold (a percentage from 0 to 100). It fails when the
// efficacy is below it, when there is none (a report that was read but holds no mutant that was run
// and decided has nothing to judge, and a scope that went empty must not pass silently), and when the
// report or the threshold cannot be read.
func writeCheck(path, threshold string, stdout, stderr io.Writer) int {
	limit, err := strconv.ParseFloat(threshold, 64)
	if err != nil || !(limit >= 0 && limit <= 100) { // written so that NaN is refused too
		_, _ = fmt.Fprintf(stderr, "mutationreport: the threshold %q is not a percentage from 0 to 100\n", threshold)
		return exitUsage
	}
	r, err := readReport(path)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "mutationreport: %s\n", describe(err))
		return exitFailure
	}
	t := tallyOf(r)
	figures := fmt.Sprintf("killed %d, timed out %d, lived %d", t.killed, t.timedOut, t.lived)
	pct, ok := t.efficacy()
	switch {
	case !ok:
		_, _ = fmt.Fprintf(stdout, "mutation efficacy n/a (%s) against the threshold %s%%: no mutant was run and decided, there is nothing to judge: FAIL\n", figures, threshold)
		return exitFailure
	case pct < limit:
		_, _ = fmt.Fprintf(stdout, "mutation efficacy %s (%s) is below the threshold %s%%: FAIL\n", t.efficacyText(), figures, threshold)
		return exitFailure
	}
	_, _ = fmt.Fprintf(stdout, "mutation efficacy %s (%s) reaches the threshold %s%%: ok\n", t.efficacyText(), figures, threshold)
	return exitOK
}
