package main

import (
	"strings"
	"testing"
)

func TestTheExtendedSummaryShowsSurvivorsEfficacyAndTheFilesTheyAreIn(t *testing.T) {
	code, out, _ := runWith(t, "extended", writeReport(t, sample))
	want := "### Mutation testing, extended operators (informational, no threshold)\n\n" +
		"KILLED: 2, LIVED: 2, NOT COVERED: 1\n\n" +
		"**Survivors: 2.** Efficacy: 50.0% (killed 2 of the 4 that were run and decided).\n\n" +
		"Survivors per file:\n\n- `a.go`: 1\n- `b.go`: 1\n\n" +
		"Surviving mutants (a behavior no test pins; an equivalent mutant is not a reason to bend a test):\n\n" +
		"- `a.go:9:2` ARITHMETIC_BASE\n- `b.go:1:1` CONDITIONALS_BOUNDARY\n"
	if code != exitOK || out != want {
		t.Errorf("code %d, output:\n%q\nwant:\n%q", code, out, want)
	}
}

func TestTheFilesWithMostSurvivorsComeFirstAndTiesKeepFileOrder(t *testing.T) {
	report := `{"files":[
 {"file_name":"z.go","mutations":[{"type":"X","status":"LIVED","line":1,"column":1}]},
 {"file_name":"m.go","mutations":[{"type":"X","status":"LIVED","line":1,"column":1},{"type":"X","status":"LIVED","line":2,"column":1},{"type":"X","status":"KILLED","line":3,"column":1}]},
 {"file_name":"a.go","mutations":[{"type":"X","status":"LIVED","line":1,"column":1},{"type":"X","status":"KILLED","line":2,"column":1}]},
 {"file_name":"clean.go","mutations":[{"type":"X","status":"KILLED","line":1,"column":1}]}
]}`
	_, out, _ := runWith(t, "extended", writeReport(t, report))
	want := "Survivors per file:\n\n- `m.go`: 2\n- `a.go`: 1\n- `z.go`: 1\n"
	if !strings.Contains(out, want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
	if strings.Contains(out, "clean.go") {
		t.Errorf("a file without survivors is listed:\n%s", out)
	}
	if !strings.Contains(out, "**Survivors: 4.** Efficacy: 42.9% (killed 3 of the 7") {
		t.Errorf("wrong figures:\n%s", out)
	}
}

func TestTheExtendedSummaryOfARunWithoutSurvivorsListsNone(t *testing.T) {
	_, out, _ := runWith(t, "extended", writeReport(t, `{"files":[{"file_name":"a.go","mutations":[{"type":"X","status":"KILLED","line":1,"column":1}]}]}`))
	if !strings.Contains(out, "**Survivors: 0.** Efficacy: 100.0% (killed 1 of the 1") {
		t.Errorf("wrong figures:\n%s", out)
	}
	if strings.Contains(out, "Survivors per file") || strings.Contains(out, "Surviving mutants") {
		t.Errorf("a list of survivors where there are none:\n%s", out)
	}
}

func TestTheExtendedSummaryOfAnEmptyOrUnreadableReportIsSaidAndNotAnError(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"no mutants", writeReport(t, `{"files":[]}`), "No mutants in scope.\n\n**Survivors: 0.** Efficacy: n/a (killed 0 of the 0"},
		{"missing report", "/nonexistent/mutation-extended.json", "No report: the run stopped before it wrote one."},
		{"broken report", writeReport(t, "not json"), "The report cannot be read:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out, errOut := runWith(t, "extended", tt.path)
			if code != exitOK || errOut != "" || !strings.Contains(out, tt.want) {
				t.Errorf("code %d, stderr %q, output:\n%s\nwant it to contain %q", code, errOut, out, tt.want)
			}
		})
	}
}
