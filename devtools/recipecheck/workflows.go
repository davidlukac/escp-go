package main

import (
	"fmt"
	"slices"
	"strings"
)

// The workflows whose zizmor job the check reads, and the job's own key.
var zizmorWorkflows = []string{".github/workflows/test.yml", ".github/workflows/weekly.yml"}

const zizmorJobKey = "  zizmor:"

// zizmorSteps are the two steps of the zizmor job that run the gate, as they must be written, whole and in
// this order: the audit (`make zizmor`, with the token the job has) and the probes (`make zizmorcheck`, which
// cannot be skipped there). The check compares these lines; a step with another command, another or an added
// `env:` variable, an `if:` or a `continue-on-error:` is not these lines.
//
// What the check is: a tripwire against an accidental or partial edit of the gate. It looks at the lines of
// these two steps, at the keys of jobLinesThatWeakenIt at the level of the job, and (in check.go) at the text
// of some recipes of the Makefile and the lines `make -n` prints for them. It does not examine the settings of
// the workflow, of the job and of make that decide whether and where the steps run. The whole workflow files
// and the Makefile are read at review, and a required status check is what makes a red job block; both are
// the maintainer's.
var zizmorSteps = []string{
	"      - name: Audit the workflows",
	"        run: make zizmor",
	"        env:",
	"          GH_TOKEN: ${{ github.token }}",
	"      - name: Prove that the options hold",
	"        run: make zizmorcheck",
	"        env:",
	"          ZIZMOR_REQUIRED: \"1\"",
}

// jobLinesThatWeakenIt are lines at the level of the zizmor job itself that would change what its steps do:
// an `env:` for the whole job, a `continue-on-error:` (a red job that does not block), an `if:`.
var jobLinesThatWeakenIt = []string{"    env:", "    continue-on-error:", "    if:"}

// workflowProblems compares the lines of what the CI jobs run, within the limits given at zizmorSteps. The
// zizmor job of a workflow must hold the steps of zizmorSteps exactly once, in a row, and must not carry a line
// of jobLinesThatWeakenIt at its own level. The text of the workflow is read as lines, since the check must
// not trust a parser that the same file could confuse; the job is the lines from its key to the next key of
// the same level (see endsJob).
func workflowProblems(name, text string) []string {
	lines := strings.Split(text, "\n")
	start := slices.Index(lines, zizmorJobKey)
	if start < 0 {
		return []string{fmt.Sprintf("%s has no zizmor job (a line %q)", name, zizmorJobKey)}
	}
	job := lines[start+1:]
	if end := slices.IndexFunc(job, endsJob); end >= 0 {
		job = job[:end]
	}
	var problems []string
	if found := countRun(job, zizmorSteps); found != 1 {
		problems = append(problems, fmt.Sprintf("%s: the zizmor job does not run the gate as pinned (the lines %q, in a row, exactly once; found %d times)", name, zizmorSteps, found))
	}
	for _, line := range job {
		for _, weak := range jobLinesThatWeakenIt {
			if strings.HasPrefix(line, weak) {
				problems = append(problems, fmt.Sprintf("%s: the zizmor job has a line %q at its own level", name, strings.TrimRight(line, " ")))
			}
		}
	}
	return problems
}

// endsJob is whether line is the key of the next job: it has exactly two leading spaces, is not a comment and
// is not blank. A line of white space only (of any length), an empty line and a comment line belong to the job
// they are in, whatever the indentation, so that none of them hides the keys that follow it.
func endsJob(line string) bool {
	return strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") && !strings.HasPrefix(line, "  #") &&
		strings.TrimSpace(line) != ""
}

// countRun is how many times the lines of want are in lines, in a row and each whole, and not followed by a line
// that goes on with the last one (a deeper indentation: another variable of the same `env:`).
func countRun(lines, want []string) int {
	count := 0
	for i := 0; i+len(want) <= len(lines); i++ {
		if !slices.Equal(lines[i:i+len(want)], want) {
			continue
		}
		if next := i + len(want); next < len(lines) && strings.HasPrefix(lines[next], "          ") {
			continue
		}
		count++
	}
	return count
}
