package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// reportOf is a gremlins report with the given number of mutants per outcome, in one file.
func reportOf(killed, timedOut, lived, notCovered int) string {
	var mutants []string
	add := func(n int, status string) {
		for range n {
			mutants = append(mutants, fmt.Sprintf(`{"type":"X","status":%q,"line":%d,"column":1}`, status, len(mutants)+1))
		}
	}
	add(killed, "KILLED")
	add(timedOut, "TIMED OUT")
	add(lived, "LIVED")
	add(notCovered, "NOT COVERED")
	return `{"files":[{"file_name":"a.go","mutations":[` + strings.Join(mutants, ",") + `]}]}`
}

const timedOutLine = "**Timed out: %d** (counted as killed; an unexplained rise is investigated: a slow test, a cached baseline, a real infinite loop)."

// The cases pin the definition: (killed + timed out) over (killed + timed out + lived). Gremlins' own
// figure, killed over (killed + lived), would say the opposite of "passes" for the first, second and
// fifth case.
var efficacyCases = []struct {
	name                           string
	killed, timedOut, lived, uncov int
	threshold                      string
	wantCode                       int
	wantText                       string // the efficacy as printed
	wantDecided                    bool
}{
	{"only timeouts remain: gremlins has no figure, this passes", 0, 5, 0, 0, "90", exitOK, "100.0%", true},
	{"timeouts lift a run over the threshold", 8, 2, 1, 0, "90", exitOK, "90.9%", true},
	{"lived mutants dominate", 5, 1, 4, 0, "90", exitFailure, "60.0%", true},
	{"timeouts do not hide survivors", 0, 1, 9, 0, "90", exitFailure, "10.0%", true},
	{"timeouts alone, gremlins would divide by zero", 0, 3, 0, 2, "90", exitOK, "100.0%", true},
	{"exactly at the threshold", 80, 10, 10, 0, "90", exitOK, "90.0%", true},
	{"one mutant below the threshold", 80, 9, 11, 0, "90", exitFailure, "89.0%", true},
	{"one mutant above the threshold", 80, 11, 9, 0, "90", exitOK, "91.0%", true},
	{"no timeouts, exactly at the threshold", 9, 0, 1, 0, "90", exitOK, "90.0%", true},
	{"no timeouts, below the threshold", 89, 0, 11, 0, "90", exitFailure, "89.0%", true},
	{"a fractional threshold", 9, 0, 1, 0, "90.5", exitFailure, "90.0%", true},
	{"threshold zero passes any figure", 0, 0, 4, 0, "0", exitOK, "0.0%", true},
	{"threshold 100 needs every mutant caught", 99, 0, 1, 0, "100", exitFailure, "99.0%", true},
	{"threshold 100 met", 3, 2, 0, 0, "100", exitOK, "100.0%", true},
	{"no mutants at all", 0, 0, 0, 0, "90", exitFailure, "n/a", false},
	{"only not covered mutants", 0, 0, 0, 4, "0", exitFailure, "n/a", false},
}

func TestTheEfficacyCountsATimedOutMutantAsKilled(t *testing.T) {
	for _, tc := range efficacyCases {
		t.Run(tc.name, func(t *testing.T) {
			tally := tallyOf(mustParse(t, reportOf(tc.killed, tc.timedOut, tc.lived, tc.uncov)))
			if tally.killed != tc.killed || tally.timedOut != tc.timedOut || tally.lived != tc.lived {
				t.Fatalf("tally %+v", tally)
			}
			if _, ok := tally.efficacy(); ok != tc.wantDecided {
				t.Errorf("efficacy defined %v, want %v", ok, tc.wantDecided)
			}
			if got := tally.efficacyText(); got != tc.wantText {
				t.Errorf("efficacy %s, want %s", got, tc.wantText)
			}
		})
	}
}

func mustParse(t *testing.T, text string) report {
	t.Helper()
	r, err := readReport(writeReport(t, text))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCheckFailsBelowTheThresholdAndPrintsOneLine(t *testing.T) {
	for _, tc := range efficacyCases {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runWith(t, "check", writeReport(t, reportOf(tc.killed, tc.timedOut, tc.lived, tc.uncov)), tc.threshold)
			if code != tc.wantCode || errOut != "" {
				t.Fatalf("code %d (want %d), stderr %q, stdout %q", code, tc.wantCode, errOut, out)
			}
			if strings.Count(out, "\n") != 1 || !strings.HasPrefix(out, "mutation efficacy "+tc.wantText+" (") {
				t.Errorf("not one line with the efficacy %s: %q", tc.wantText, out)
			}
			figures := fmt.Sprintf("killed %d, timed out %d, lived %d", tc.killed, tc.timedOut, tc.lived)
			if !strings.Contains(out, figures) || !strings.Contains(out, tc.threshold+"%") {
				t.Errorf("line lacks %q or the threshold %s: %q", figures, tc.threshold, out)
			}
			wantVerdict := map[int]string{exitOK: ": ok\n", exitFailure: ": FAIL\n"}[tc.wantCode]
			if !strings.HasSuffix(out, wantVerdict) {
				t.Errorf("line does not end with %q: %q", wantVerdict, out)
			}
		})
	}
}

// The summary, the badge and the check read one function: they cannot disagree on a report.
func TestTheBadgeAndTheCheckAgreeOnTheEfficacy(t *testing.T) {
	for _, tc := range efficacyCases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeReport(t, reportOf(tc.killed, tc.timedOut, tc.lived, tc.uncov))
			_, badgeOut, _ := runWith(t, "badge", path)
			_, checkOut, _ := runWith(t, "check", path, tc.threshold)
			_, summaryOut, _ := runWith(t, path)
			_, extendedOut, _ := runWith(t, "extended", path)
			if !strings.Contains(badgeOut, `"message":"`+tc.wantText+`"`) {
				t.Errorf("badge %q lacks %s", badgeOut, tc.wantText)
			}
			if !strings.Contains(checkOut, "efficacy "+tc.wantText+" ") {
				t.Errorf("check %q lacks %s", checkOut, tc.wantText)
			}
			for _, out := range []string{summaryOut, extendedOut} { // subtests: the two summaries are two checks of one case
				if !strings.Contains(out, "Efficacy: "+tc.wantText+" (") {
					t.Errorf("summary lacks %s:\n%s", tc.wantText, out)
				}
			}
		})
	}
}

func TestACheckWithABadThresholdIsAUsageError(t *testing.T) {
	path := writeReport(t, reportOf(9, 0, 1, 0))
	for _, threshold := range []string{"", "abc", "-1", "100.5", "101", "NaN", "+Inf", "9 0"} {
		t.Run(threshold, func(t *testing.T) {
			code, out, errOut := runWith(t, "check", path, threshold)
			if code != exitUsage || out != "" || !strings.Contains(errOut, "not a percentage from 0 to 100") {
				t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
			}
		})
	}
}

// A check that cannot read its report must fail: a threshold nobody evaluated is not a pass.
func TestACheckOfAMissingOrBrokenReportFails(t *testing.T) {
	tests := []struct {
		name, path, want string
	}{
		{"missing", filepath.Join(t.TempDir(), "absent.json"), "no report"},
		{"broken", writeReport(t, `{`), "the report cannot be read"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errOut := runWith(t, "check", tt.path, "90")
			if code != exitFailure || out != "" || !strings.Contains(errOut, tt.want) {
				t.Errorf("code %d, stdout %q, stderr %q", code, out, errOut)
			}
		})
	}
}

func TestCheckNeedsAReportAndAThreshold(t *testing.T) {
	for _, args := range [][]string{{"check"}, {"check", "a"}, {"check", "a", "90", "x"}, {"badge"}, {"extended"}} {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			if code, _, errOut := runWith(t, args...); code != exitUsage || !strings.Contains(errOut, "usage: mutationreport REPORT.json") {
				t.Errorf("code %d, stderr %q", code, errOut)
			}
		})
	}
}

const timedOutReport = `{"files":[
 {"file_name":"a.go","mutations":[{"type":"CONDITIONALS_NEGATION","status":"TIMED OUT","line":3,"column":4},{"type":"ARITHMETIC_BASE","status":"LIVED","line":9,"column":2}]},
 {"file_name":"b.go","mutations":[{"type":"INCREMENT_DECREMENT","status":"TIMED OUT","line":7,"column":1},{"type":"X","status":"KILLED","line":1,"column":1}]}
]}`

func TestTheSummariesShowTheTimedOutCountAndTheMutants(t *testing.T) {
	timedOutHeading := "Timed out mutants (counted as killed; the tests did not finish, which is a kill, but check that the cause is the mutant and not a slow test):"
	for _, command := range [][]string{{}, {"extended"}} {
		t.Run(fmt.Sprint(command), func(t *testing.T) {
			_, out, _ := runWith(t, append(command, writeReport(t, timedOutReport))...)
			for _, want := range []string{ // subtests: the fragments are checks of one output, not cases
				"KILLED: 1, LIVED: 1, TIMED OUT: 2",
				"**Survivors: 1.** Efficacy: 75.0% (killed 1 and timed out 2 of the 4 that were run and decided).",
				fmt.Sprintf(timedOutLine, 2),
				timedOutHeading + "\n\n- `a.go:3:4` CONDITIONALS_NEGATION\n- `b.go:7:1` INCREMENT_DECREMENT\n",
				"Surviving mutants (a behavior no test pins; an equivalent mutant is not a reason to bend a test):\n\n- `a.go:9:2` ARITHMETIC_BASE\n",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("summary lacks %q:\n%s", want, out)
				}
			}
			if strings.Index(out, timedOutHeading) > strings.Index(out, "Surviving mutants") {
				t.Errorf("the timed-out mutants are not listed before the survivors:\n%s", out)
			}
		})
	}
}

func TestTheSummariesListNoTimedOutMutantsWhenThereAreNone(t *testing.T) {
	for _, command := range [][]string{{}, {"extended"}} {
		t.Run(fmt.Sprint(command), func(t *testing.T) {
			_, out, _ := runWith(t, append(command, writeReport(t, reportOf(3, 0, 1, 0)))...)
			if !strings.Contains(out, fmt.Sprintf(timedOutLine, 0)) || strings.Contains(out, "Timed out mutants") {
				t.Errorf("want the count of zero and no list:\n%s", out)
			}
		})
	}
}
