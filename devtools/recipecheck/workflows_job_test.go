package main

import (
	"strings"
	"testing"
)

// jobCase is a workflow text and the word of the problem it must be reported with ("" for none).
type jobCase struct {
	name     string
	workflow string
	want     string
}

// The job ends at the next key of the same level and at nothing else: a line of white space only, an empty line
// and a comment line are inside the job, and the keys after them are still the job's own.
func TestTheZizmorJobEndsAtTheNextJobKeyOnly(t *testing.T) {
	steps := strings.Join(zizmorSteps, "\n")
	afterName := "    name: Workflow security (zizmor)\n"
	// before and after put a line of the given text and then a forbidden job-level key into the job, before
	// and after its pinned steps.
	before := func(text string) string {
		return strings.Replace(goodWorkflow, afterName, afterName+text+"\n    if: false\n", 1)
	}
	after := func(text string) string {
		return strings.Replace(goodWorkflow, steps+"\n\n  unit:", steps+"\n"+text+"\n    if: false\n\n  unit:", 1)
	}
	cases := []jobCase{
		{name: "a forbidden key directly after the key of the job", workflow: strings.Replace(goodWorkflow, zizmorJobKey+"\n", zizmorJobKey+"\n    if: false\n", 1), want: "if: false"},
		{name: "a forbidden key directly after the key of the job, whole file", workflow: zizmorJobKey + "\n    continue-on-error: true\n" + steps, want: "continue-on-error:"},
		{name: "the job has no line and the steps are in the job after it", workflow: "jobs:\n" + zizmorJobKey + "\n  unit:\n    runs-on: x\n    steps:\n" + steps + "\n", want: "does not run the gate as pinned"},
		{name: "the job has no line and the next job holds a forbidden key", workflow: "jobs:\n" + zizmorJobKey + "\n  unit:\n    if: false\n", want: "found 0 times"},
		{name: "an empty line before a forbidden key", workflow: before(""), want: "if: false"},
		{name: "an empty line after the steps, before a forbidden key", workflow: after(""), want: "if: false"},
		{name: "a comment before a forbidden key", workflow: before("  # note"), want: "if: false"},
		{name: "a comment after the steps, before a forbidden key", workflow: after("  # note"), want: "if: false"},
		{name: "a comment with nothing after the marker, before a forbidden key", workflow: after("  #"), want: "if: false"},
		{name: "the next job key is the end", workflow: strings.Replace(goodWorkflow, "\n  unit:\n    runs-on: x\n", "\n  unit:\n    runs-on: x\n    if: false\n", 1), want: ""},
		{name: "a key of the next job is not this job's", workflow: strings.Replace(goodWorkflow, "    steps:\n      - run: make test\n", "    if: false\n    steps:\n      - run: make test\n", 1), want: ""},
		{name: "the next job key is the last line, without an end of line", workflow: goodWorkflow[:strings.Index(goodWorkflow, "  unit:")+len("  unit:")], want: ""},
		{name: "a line of two spaces is the last line, without an end of line", workflow: goodWorkflow[:strings.Index(goodWorkflow, steps)+len(steps)] + "\n  ", want: ""},
		{name: "a forbidden key is the last line, without an end of line", workflow: goodWorkflow[:strings.Index(goodWorkflow, steps)+len(steps)] + "\n    if: false", want: "if: false"},
		{name: "a line of two spaces, then a forbidden key that is the last line, without an end of line", workflow: goodWorkflow[:strings.Index(goodWorkflow, steps)+len(steps)] + "\n  \n    if: false", want: "if: false"},
		{name: "a line of two spaces and a tab before a forbidden key", workflow: after("  \t"), want: "if: false"},
		{name: "a line of two spaces and a carriage return before a forbidden key", workflow: after("  \r"), want: "if: false"},
	}
	for _, space := range []struct{ name, text string }{
		{name: "one space", text: " "},
		{name: "two spaces", text: "  "},
		{name: "three spaces", text: "   "},
		{name: "four spaces", text: "    "},
		{name: "a tab", text: "\t"},
	} {
		cases = append(cases,
			jobCase{name: "a line of " + space.name + " before the steps, then a forbidden key", workflow: before(space.text), want: "if: false"},
			jobCase{name: "a line of " + space.name + " after the steps, then a forbidden key", workflow: after(space.text), want: "if: false"},
			jobCase{name: "a line of " + space.name + " between the key of the job and the first line, then a forbidden key", workflow: strings.Replace(goodWorkflow, zizmorJobKey+"\n", zizmorJobKey+"\n"+space.text+"\n    if: false\n", 1), want: "if: false"},
			jobCase{name: "a line of " + space.name + " before the next job key", workflow: strings.Replace(goodWorkflow, "\n\n  unit:", "\n"+space.text+"\n  unit:", 1), want: ""},
		)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			problems := workflowProblems("w.yml", tc.workflow)
			switch {
			case tc.want == "" && len(problems) != 0:
				t.Errorf("problems %q, want none", problems)
			case tc.want != "" && !strings.Contains(strings.Join(problems, "\n"), tc.want):
				t.Errorf("problems %q, want one with %q", problems, tc.want)
			}
		})
	}
}

// endsJob is true for the key of a job and for nothing else: not for a line of white space, an empty line, a
// comment or a line indented further.
func TestEndsJob(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		want bool
	}{
		{name: "a job key", line: "  unit:", want: true},
		{name: "a job key with a trailing space", line: "  unit: ", want: true},
		{name: "an empty line", line: "", want: false},
		{name: "one space", line: " ", want: false},
		{name: "two spaces", line: "  ", want: false},
		{name: "three spaces", line: "   ", want: false},
		{name: "two spaces and a tab", line: "  \t", want: false},
		{name: "a tab", line: "\t", want: false},
		{name: "two spaces and a carriage return", line: "  \r", want: false},
		{name: "two spaces and a form feed", line: "  \f", want: false},
		{name: "two spaces and a vertical tab", line: "  \v", want: false},
		{name: "two spaces and a no-break space", line: "   ", want: false},
		{name: "a comment", line: "  # note", want: false},
		{name: "a deeper line that has a key", line: "   unit:", want: false},
		// Not a comment for this check (the file would not be valid YAML, and the other checks refuse it).
		{name: "two spaces, a tab and a comment", line: "  \t# note", want: true},
		{name: "a job-level key", line: "    if: false", want: false},
		{name: "a one-space key", line: " unit:", want: false},
		{name: "a key of the workflow", line: "jobs:", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := endsJob(tc.line); got != tc.want {
				t.Errorf("endsJob(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}
