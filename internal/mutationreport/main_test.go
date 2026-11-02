package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `{"go_module":"m","files":[
 {"file_name":"a.go","mutations":[{"type":"CONDITIONALS_NEGATION","status":"KILLED","line":3,"column":4},{"type":"ARITHMETIC_BASE","status":"LIVED","line":9,"column":2}]},
 {"file_name":"b.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":1,"column":1},{"type":"CONDITIONALS_BOUNDARY","status":"KILLED","line":2,"column":1},{"type":"CONDITIONALS_BOUNDARY","status":"NOT COVERED","line":5,"column":1}]}
]}`

func runWith(t *testing.T, args ...string) (code int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	code = run(args, &o, &e)
	return code, o.String(), e.String()
}

func writeReport(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mutation.json")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTheSummaryCountsEachOutcomeAndListsTheSurvivors(t *testing.T) {
	code, out, errOut := runWith(t, writeReport(t, sample))
	want := "### Mutation testing\n\nKILLED: 2, LIVED: 2, NOT COVERED: 1\n\n" +
		"Surviving mutants (a behavior no test pins; an equivalent mutant is not a reason to bend a test):\n\n" +
		"- `a.go:9:2` ARITHMETIC_BASE\n- `b.go:1:1` CONDITIONALS_BOUNDARY\n"
	if code != exitOK || errOut != "" || out != want {
		t.Fatalf("code %d, stderr %q, stdout:\n%s\nwant:\n%s", code, errOut, out, want)
	}
}

func TestARunWithoutSurvivorsListsNone(t *testing.T) {
	_, out, _ := runWith(t, writeReport(t, `{"files":[{"file_name":"a.go","mutations":[{"type":"X","status":"KILLED","line":1,"column":1}]}]}`))
	if !strings.Contains(out, "KILLED: 1") || strings.Contains(out, "Surviving") {
		t.Fatalf("got %q", out)
	}
}

func TestAnEmptyReportSaysThereWasNothingToMutate(t *testing.T) {
	_, out, _ := runWith(t, writeReport(t, `{"files":[]}`))
	if !strings.Contains(out, "No mutants in scope.") {
		t.Fatalf("got %q", out)
	}
}

// A job summary must never fail the job: a missing or unreadable report is said, not an error.
func TestAMissingOrBrokenReportIsSaidAndNotAnError(t *testing.T) {
	code, out, _ := runWith(t, filepath.Join(t.TempDir(), "absent.json"))
	if code != exitOK || !strings.Contains(out, "No report") {
		t.Fatalf("missing: code %d, %q", code, out)
	}
	code, out, _ = runWith(t, writeReport(t, `{`))
	if code != exitOK || !strings.Contains(out, "cannot be read") {
		t.Fatalf("broken: code %d, %q", code, out)
	}
}

func TestWrongArgumentsAreAUsageError(t *testing.T) {
	for _, args := range [][]string{nil, {"a", "b"}} {
		if code, _, errOut := runWith(t, args...); code != exitUsage || !strings.Contains(errOut, "usage") {
			t.Errorf("%v: code %d, %q", args, code, errOut)
		}
	}
}

func TestTheBadgeIsTheEfficacyOfTheRun(t *testing.T) {
	// 2 killed and 2 survived: 50%, red; the not covered mutant is not counted
	code, out, errOut := runWith(t, "badge", writeReport(t, sample))
	want := `{"schemaVersion":1,"label":"mutation score","message":"50.0%","color":"red"}` + "\n"
	if code != exitOK || errOut != "" || out != want {
		t.Fatalf("code %d, stderr %q, stdout %q, want %q", code, errOut, out, want)
	}
}

func TestTheBadgeColorFollowsTheBands(t *testing.T) {
	for _, tc := range []struct {
		killed, lived  int
		message, color string
	}{
		{100, 0, "100.0%", "brightgreen"},
		{95, 5, "95.0%", "brightgreen"},
		{94, 6, "94.0%", "green"},
		{90, 10, "90.0%", "green"},
		{89, 11, "89.0%", "yellowgreen"},
		{80, 20, "80.0%", "yellowgreen"},
		{70, 30, "70.0%", "yellow"},
		{60, 40, "60.0%", "orange"},
		{59, 41, "59.0%", "red"},
		{0, 3, "0.0%", "red"},
	} {
		var r report
		r.Files = make([]struct {
			Name      string `json:"file_name"`
			Mutations []struct {
				Type   string `json:"type"`
				Status string `json:"status"`
				Line   int    `json:"line"`
				Column int    `json:"column"`
			} `json:"mutations"`
		}, 1)
		for i := range tc.killed + tc.lived {
			status := statusKilled
			if i >= tc.killed {
				status = statusLived
			}
			r.Files[0].Mutations = append(r.Files[0].Mutations, struct {
				Type   string `json:"type"`
				Status string `json:"status"`
				Line   int    `json:"line"`
				Column int    `json:"column"`
			}{Status: status})
		}
		if got := badgeOf(r); got.Message != tc.message || got.Color != tc.color {
			t.Errorf("%d killed, %d survived: %s %s, want %s %s", tc.killed, tc.lived, got.Message, got.Color, tc.message, tc.color)
		}
	}
}

func TestTheBadgeOfARunWithoutDecidedMutantsIsNotApplicable(t *testing.T) {
	_, out, _ := runWith(t, "badge", writeReport(t, `{"files":[{"file_name":"a.go","mutations":[{"type":"X","status":"NOT COVERED","line":1,"column":1}]}]}`))
	if !strings.Contains(out, `"message":"n/a"`) || !strings.Contains(out, `"color":"lightgrey"`) {
		t.Fatalf("got %q", out)
	}
}

// A badge is only ever published from a report that was read: otherwise the exit code says so.
func TestABadgeFromAMissingOrBrokenReportFails(t *testing.T) {
	if code, out, errOut := runWith(t, "badge", filepath.Join(t.TempDir(), "absent.json")); code != exitFailure || out != "" || errOut == "" {
		t.Errorf("missing: code %d, stdout %q, stderr %q", code, out, errOut)
	}
	if code, out, errOut := runWith(t, "badge", writeReport(t, `{`)); code != exitFailure || out != "" || !strings.Contains(errOut, "cannot be read") {
		t.Errorf("broken: code %d, stdout %q, stderr %q", code, out, errOut)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestABadgeThatCannotBeWrittenFails(t *testing.T) {
	var e bytes.Buffer
	if code := run([]string{"badge", writeReport(t, sample)}, failingWriter{}, &e); code != exitFailure || e.Len() == 0 {
		t.Fatalf("code %d, stderr %q", code, e.String())
	}
}
