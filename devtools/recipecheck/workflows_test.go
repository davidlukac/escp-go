package main

import (
	"strings"
	"testing"
)

func TestTheZizmorJobOfAWorkflowIsPinned(t *testing.T) {
	step := strings.Join(zizmorSteps, "\n")
	probes := strings.Join(zizmorSteps[4:], "\n") + "\n"
	for _, tc := range []struct {
		name     string
		workflow string
		want     string // a word of the problem, "" for none
	}{
		{name: "as it is", workflow: goodWorkflow, want: ""},
		{name: "a comment at the level of the jobs inside the job", workflow: strings.Replace(goodWorkflow, "      - uses: actions/checkout@abc\n", "  # a note between two steps\n      - uses: actions/checkout@abc\n", 1), want: ""},
		{name: "a comment at the level of the jobs between the two pinned steps is not part of them", workflow: strings.Replace(goodWorkflow, "      - name: Prove that the options hold", "  # a note\n      - name: Prove that the options hold", 1), want: "does not run the gate as pinned"},
		{name: "a blank line inside the job", workflow: strings.Replace(goodWorkflow, "      - uses: actions/checkout@abc\n", "\n      - uses: actions/checkout@abc\n", 1), want: ""},
		{name: "the words of the job key in a line before it", workflow: strings.Replace(goodWorkflow, "      - run: make lint", "      - run: make lint # the  zizmor: job is below", 1), want: ""},
		{name: "the steps are the last lines of the file, without an end of line", workflow: goodWorkflow[:strings.Index(goodWorkflow, step)+len(step)], want: ""},
		{name: "the steps are the last lines of the file, with an end of line", workflow: goodWorkflow[:strings.Index(goodWorkflow, step)+len(step)] + "\n", want: ""},
		{name: "the steps are the last lines of the file and something follows them", workflow: goodWorkflow[:strings.Index(goodWorkflow, step)+len(step)] + "\n          ZIZMOR_CONFIG: x", want: "does not run the gate as pinned"},
		{name: "the audit replaced by true", workflow: strings.Replace(goodWorkflow, "run: make zizmor\n", "run: \"true\"\n", 1), want: "does not run the gate as pinned"},
		{name: "the audit made non-fatal", workflow: strings.Replace(goodWorkflow, "run: make zizmor\n", "run: make zizmor || true\n", 1), want: "does not run the gate as pinned"},
		{name: "the audit with a number of its own", workflow: strings.Replace(goodWorkflow, "run: make zizmor\n", "run: make zizmor ZIZMOR_SELF_REPOSITORY_FINDINGS=99\n", 1), want: "does not run the gate as pinned"},
		{name: "the audit with another target", workflow: strings.Replace(goodWorkflow, "run: make zizmor\n", "run: make actionlint\n", 1), want: "does not run the gate as pinned"},
		{name: "the probes dropped", workflow: strings.Replace(goodWorkflow, probes, "", 1), want: "does not run the gate as pinned"},
		{name: "the probes made non-fatal", workflow: strings.Replace(goodWorkflow, "run: make zizmorcheck\n", "run: make zizmorcheck || true\n", 1), want: "does not run the gate as pinned"},
		{name: "the probes allowed to be skipped", workflow: strings.Replace(goodWorkflow, `ZIZMOR_REQUIRED: "1"`, `ZIZMOR_REQUIRED: ""`, 1), want: "does not run the gate as pinned"},
		{name: "the token of the audit dropped", workflow: strings.Replace(goodWorkflow, "          GH_TOKEN: ${{ github.token }}\n", "", 1), want: "does not run the gate as pinned"},
		{name: "a variable added to the audit that clears nothing but changes the run", workflow: strings.Replace(goodWorkflow, "          GH_TOKEN: ${{ github.token }}\n", "          GH_TOKEN: ${{ github.token }}\n          ZIZMOR_OFFLINE: \"true\"\n", 1), want: "does not run the gate as pinned"},
		{name: "a variable added to the probes", workflow: strings.Replace(goodWorkflow, "          ZIZMOR_REQUIRED: \"1\"", "          ZIZMOR_REQUIRED: \"1\"\n          ZIZMOR_CONFIG: x", 1), want: "does not run the gate as pinned"},
		{name: "a condition on the audit step", workflow: strings.Replace(goodWorkflow, "        run: make zizmor\n", "        if: false\n        run: make zizmor\n", 1), want: "does not run the gate as pinned"},
		{name: "the two steps apart", workflow: strings.Replace(goodWorkflow, "      - name: Prove that the options hold", "      - run: echo between\n      - name: Prove that the options hold", 1), want: "does not run the gate as pinned"},
		{name: "the steps twice", workflow: strings.Replace(goodWorkflow, "\n  unit:", "\n"+step+"\n\n  unit:", 1), want: "found 2 times"},
		{name: "a condition on the job", workflow: strings.Replace(goodWorkflow, "    name: Workflow security (zizmor)\n", "    name: Workflow security (zizmor)\n    if: false\n", 1), want: "if: false"},
		{name: "a red job that does not block", workflow: strings.Replace(goodWorkflow, "    name: Workflow security (zizmor)\n", "    name: Workflow security (zizmor)\n    continue-on-error: true\n", 1), want: "continue-on-error:"},
		{name: "an environment for the whole job", workflow: strings.Replace(goodWorkflow, "    name: Workflow security (zizmor)\n", "    name: Workflow security (zizmor)\n    env:\n      ZIZMOR_OFFLINE: \"true\"\n", 1), want: `"    env:" at its own level`},
		{name: "a weakening line in another job is not this job's", workflow: strings.Replace(goodWorkflow, "    runs-on: x\n    steps:\n      - run: make lint", "    runs-on: x\n    if: false\n    env:\n      A: b\n    steps:\n      - run: make lint", 1), want: ""},
		{name: "no zizmor job", workflow: strings.Replace(goodWorkflow, zizmorJobKey, "  other:", 1), want: "has no zizmor job"},
		{name: "the steps are in the job after it", workflow: strings.Replace(goodWorkflow, step+"\n\n  unit:\n    runs-on: x\n    steps:\n      - run: make test\n", "\n  unit:\n    runs-on: x\n    steps:\n      - run: make test\n"+step+"\n", 1), want: "does not run the gate as pinned"},
	} {
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

// The check reads both workflows: a problem in either is reported with its name, and a workflow that cannot be
// read is an error and not a pass.
func TestBothWorkflowsAreRead(t *testing.T) {
	for _, name := range zizmorWorkflows {
		t.Run(name, func(t *testing.T) {
			m := newIdealMake(t)
			c := newChecker(t, m)
			bad := strings.Replace(goodWorkflow, "run: make zizmor\n", "run: \"true\"\n", 1)
			if err := c.files.WriteFile(c.root+"/"+name, []byte(bad), fileMode); err != nil {
				t.Fatal(err)
			}
			problems, err := c.check(t.Context())
			if err != nil || len(problems) != 1 || !strings.Contains(problems[0], name) {
				t.Errorf("problems %q, error %v, want one that names %s", problems, err, name)
			}
		})
		t.Run(name+" missing", func(t *testing.T) {
			m := newIdealMake(t)
			c := newChecker(t, m)
			if err := c.files.Remove(c.root + "/" + name); err != nil {
				t.Fatal(err)
			}
			if _, err := c.check(t.Context()); err == nil || !strings.Contains(err.Error(), name) {
				t.Errorf("error = %v, want one that names %s", err, name)
			}
		})
	}
}
