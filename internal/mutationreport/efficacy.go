package main

import (
	"fmt"
	"io"
	"regexp"
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

// efficacyText is the efficacy as a percentage with two decimals, "n/a" when there is none. Two, and
// the exact counts beside it in the check line, because the verdict compares the unrounded value: 1799
// of 1999 is 89.995%, below 90, and one decimal would print it as 90.0%.
func (t tally) efficacyText() string {
	pct, ok := t.efficacy()
	if !ok {
		return "n/a"
	}
	return fmt.Sprintf("%.2f%%", pct)
}

// parseThreshold reads a threshold, a percentage from 0 to 100.
func parseThreshold(text string) (float64, error) {
	limit, err := strconv.ParseFloat(text, 64)
	if err != nil || !(limit >= 0 && limit <= 100) { // written so that NaN is refused too
		return 0, fmt.Errorf("the threshold %q is not a percentage from 0 to 100", text)
	}
	return limit, nil
}

// writeThreshold is the `threshold` command: it exits 0 when text is a valid threshold and says why
// on stderr, with exit code 2, when it is not. `make mutation` runs it before the quarter of an hour
// of mutation, so that a typo in EFFICACY fails at once and not after the run.
func writeThreshold(text string, stderr io.Writer) int {
	if _, err := parseThreshold(text); err != nil {
		_, _ = fmt.Fprintf(stderr, "mutationreport: %v\n", err)
		return exitUsage
	}
	return exitOK
}

// writeCheck is the `check` command: it prints one line with the figures of the report and returns
// exitOK when the efficacy reaches the threshold (a percentage from 0 to 100): it passes AT the
// threshold, where gremlins' own gate failed at equality. It fails when the
// efficacy is below it, when there is none (a report that was read but holds no mutant that was run
// and decided has nothing to judge, and a scope that went empty must not pass silently), and when the
// report or the threshold cannot be read.
func writeCheck(path, threshold string, stdout, stderr io.Writer) int {
	limit, err := parseThreshold(threshold)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "mutationreport: %v\n", err)
		return exitUsage
	}
	r, err := readReport(path)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "mutationreport: %s\n", describe(err))
		return exitFailure
	}
	t := tallyOf(r)
	figures := fmt.Sprintf("caught %d of %d: killed %d, timed out %d, lived %d",
		t.killed+t.timedOut, t.decided(), t.killed, t.timedOut, t.lived)
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

// coefficientForm is what a timeout coefficient looks like: one to four digits, the first not zero, 1
// to 9999. The bound is what keeps gremlins' budget honest: it multiplies the elapsed time of its
// coverage run by the value as a duration of nanoseconds, which overflows to a negative budget from
// about 3e8 for a run of half a minute, so every mutant would time out at once and the check, which
// counts a timeout as killed, would pass on no evidence. The plain decimal form also keeps what the
// check validates and what gremlins reads the same: it reads the flag with base 0, where `010` is
// octal 8, and takes `+5`, `0x10` and `1_0`.
var coefficientForm = regexp.MustCompile(`^[1-9][0-9]{0,3}$`)

// writeCoefficient is the `coefficient` command: it exits 0 when text is a timeout coefficient in
// that form and says why on stderr, with exit code 2, when it is not. gremlins multiplies the time of
// its coverage run by it to get the time each mutant has, and a mutant that outlives that time is
// TIMED OUT, which the efficacy counts as killed: a negative value makes every mutant time out at
// once, and gremlins takes 0 for its default 3. `make` runs it before gremlins, as it runs
// `threshold`.
func writeCoefficient(text string, stderr io.Writer) int {
	if !coefficientForm.MatchString(text) {
		_, _ = fmt.Fprintf(stderr, "mutationreport: the timeout coefficient %q is not a plain integer from 1 to 9999\n", text)
		return exitUsage
	}
	return exitOK
}
