package main

import (
	"fmt"
	"slices"
	"strings"
)

// The files and words of the recipes under check. A recipe is compared with them whole, so they are
// written out here and not derived from the Makefile.
const (
	reportFile     = "build/mutation.json"
	extendedFile   = "build/mutation-extended.json"
	reportTool     = "go run ./internal/mutationreport"
	gremlinsMarker = "gremlins unleash"
	gremlinsPrefix = "GOFLAGS=-count=1 go tool -modfile=devtools/go.mod gremlins unleash . "
	gremlinsPlace  = "GREMLINS"
	defaultBase    = "origin/main"
	defaultCoeff   = "10"
	otherCoeff     = "20"
	lintRecipe     = "go -C devtools run ./recipecheck -root "
	recipeTarget   = "mutationrecipe"
	mutantsTarget  = "recipemutants"
	recipeSource   = "\tgo -C devtools run ./recipecheck -root $(CURDIR)"
	deadcodeTarget = "deadcode"
	zizmorTarget   = "zizmorcheck"
	// zizmorCount is the number of allowed findings the lint case hands to make, to see that the recipe passes
	// the Makefile's variable on and does not hold a number of its own.
	zizmorCount = "7"
)

// zizmorRunTarget is the target the CI jobs run, `make zizmor`. zizmorRunRecipe is what `make -n zizmor
// ZIZMOR_SELF_REPOSITORY_FINDINGS=7` must print, each line whole (the number is the one given on the command
// line, so that a number of its own in the recipe fails), and zizmorRunSource is the recipe as it is written in
// the Makefile, where `make -n` hides a prefix.
const zizmorRunTarget = "zizmor"

var (
	zizmorRunRecipe = []string{
		"command -v zizmor >/dev/null 2>&1 || { \\",
		"\t\techo \"zizmor is not installed. The pinned version, with hashes:\"; \\",
		"\t\techo \"  python3 -m venv ~/.cache/escp-go-zizmor && ~/.cache/escp-go-zizmor/bin/pip install --require-hashes -r devtools/zizmor/requirements.txt\"; \\",
		"\t\techo \"then put ~/.cache/escp-go-zizmor/bin on PATH.\"; \\",
		"\t\texit 1; }",
		"pin=\"$(sed -n 's/^zizmor==\\([0-9][0-9.]*\\).*/\\1/p' devtools/zizmor/requirements.txt)\"; \\",
		"\thave=\"$(zizmor --version | awk '{print $2}')\"; \\",
		"\t[ \"$have\" = \"$pin\" ] || echo \"warning: zizmor $have is installed, devtools/zizmor/requirements.txt pins $pin\"",
		"ZIZMOR_SELF_REPOSITORY_FINDINGS=" + zizmorCount + " scripts/zizmor.sh",
	}
	zizmorRunSource = []string{
		"\t@command -v zizmor >/dev/null 2>&1 || { \\",
		"\t\techo \"zizmor is not installed. The pinned version, with hashes:\"; \\",
		"\t\techo \"  python3 -m venv ~/.cache/escp-go-zizmor && ~/.cache/escp-go-zizmor/bin/pip install --require-hashes -r devtools/zizmor/requirements.txt\"; \\",
		"\t\techo \"then put ~/.cache/escp-go-zizmor/bin on PATH.\"; \\",
		"\t\texit 1; }",
		"\t@pin=\"$$(sed -n 's/^zizmor==\\([0-9][0-9.]*\\).*/\\1/p' devtools/zizmor/requirements.txt)\"; \\",
		"\thave=\"$$(zizmor --version | awk '{print $$2}')\"; \\",
		"\t[ \"$$have\" = \"$$pin\" ] || echo \"warning: zizmor $$have is installed, devtools/zizmor/requirements.txt pins $$pin\"",
		"\t@ZIZMOR_SELF_REPOSITORY_FINDINGS=$(ZIZMOR_SELF_REPOSITORY_FINDINGS) scripts/zizmor.sh",
	}
)

// zizmorRecipe is the line `make -n lint` must print for the probes of the zizmor gate, whole, with the
// number given on the command line; zizmorSource is the recipe as it is written in the Makefile, where
// `make -n` hides a prefix.
var (
	zizmorRecipe = "ZIZMOR_SELF_REPOSITORY_FINDINGS=" + zizmorCount + " go run ./internal/zizmorprobe"
	zizmorSource = []string{"\tZIZMOR_SELF_REPOSITORY_FINDINGS=$(ZIZMOR_SELF_REPOSITORY_FINDINGS) go run ./internal/zizmorprobe"}
)

// deadcodeRecipe is what `make -n lint` must print for the dead-code check, each line whole: the
// pinned linter with the configuration that runs `unused` alone with the tests left out, with and
// without the live tag. deadcodeSource is the same recipe as it is written in the Makefile, where
// `make -n` hides a prefix.
var (
	deadcodeRecipe = []string{
		"go tool -modfile=devtools/go.mod golangci-lint run --config .golangci-deadcode.yml ./...",
		"go tool -modfile=devtools/go.mod golangci-lint run --config .golangci-deadcode.yml --build-tags live ./...",
	}
	deadcodeSource = []string{
		"\t$(DEVTOOL) golangci-lint run --config .golangci-deadcode.yml ./...",
		"\t$(DEVTOOL) golangci-lint run --config .golangci-deadcode.yml --build-tags live ./...",
	}
)

// The targets.
const (
	targetMutation = "mutation"
	targetDiff     = "mutation-diff"
	targetExtended = "mutation-extended"
)

// pins are what is required of the one line that runs gremlins, whose exclusions and package filter
// vary: it ends with ending, holds each word of has (as a whole word, or a phrase with its spaces), and
// holds none of lacks. It never holds --threshold-efficacy: the check counts a timed-out mutant as
// killed, gremlins does not.
type pins struct {
	ending string
	has    []string
	lacks  []string
}

// dryCase is one run of `make -n`: the recipe it prints must be exactly want, with the line of gremlins
// written as gremlinsPlace and held to the pins (want nil: only the pins are checked).
type dryCase struct {
	label  string
	args   []string
	env    []string
	want   []string
	pins   pins
	target string
	// plain: no gremlins in the recipe, only the whole-line comparison.
	plain bool
	// looseTabs: the lines are compared without their leading tabs. GNU make removes a tab after a backslash and
	// a new line when it prints a recipe and make 3.81 does not, so the lines of a recipe that continues a line
	// differ in their tabs between the two; the words of the lines are still compared whole. The recipe as it is
	// written in the Makefile is pinned with its tabs by sourceProblems.
	looseTabs bool
}

// thresholdLine, coefficientLine and checkLine are the steps the recipes run around gremlins.
func thresholdLine(value string) string { return fmt.Sprintf("%s threshold %q", reportTool, value) }

func coefficientLine(value string) string {
	return fmt.Sprintf("%s coefficient %q", reportTool, value)
}

func checkLine(value string) string {
	return fmt.Sprintf("%s check %s %q", reportTool, reportFile, value)
}

// recipeOf is the whole recipe of target as `make -n` prints it (gremlins as gremlinsPlace): the old
// report is deleted first (gremlins exits 0 and writes no report when it has nothing to report, so a
// stale one would be judged in place of the run), with an efficacy (a pointer: an explicitly empty one
// is a value) the threshold is validated first and the check runs after gremlins, and the coefficient
// is validated before gremlins in every target. The extended run is informational: no report removal,
// no threshold, no check.
func recipeOf(target string, efficacy *string, coefficient string) []string {
	if target == targetExtended {
		return []string{"mkdir -p build", coefficientLine(coefficient), gremlinsPlace}
	}
	lines := []string{"mkdir -p build", "rm -f " + reportFile}
	if efficacy != nil {
		lines = append(lines, thresholdLine(*efficacy))
	}
	lines = append(lines, coefficientLine(coefficient), gremlinsPlace)
	if efficacy != nil {
		lines = append(lines, checkLine(*efficacy))
	}
	return lines
}

// gremlinsPins are the pins of the gremlins line of target with the coefficient and the extra words.
func gremlinsPins(target, coefficient string, has ...string) pins {
	p := pins{has: append([]string{"GOFLAGS=-count=1", "--timeout-coefficient " + coefficient}, has...)}
	switch target {
	case targetMutation:
		p.ending = " --output " + reportFile
	case targetDiff:
		p.ending = " --output " + reportFile + " --diff " + defaultBase
	default:
		// the extended run writes its own report and ends with the last of its operators
		p.ending = "--remove-self-assignments"
		p.has = append(p.has, "--output "+extendedFile)
	}
	return p
}

// packageFilter is the exclusion `-E '^dir/[^/]+$'` the PKG filter gives gremlins for a directory.
func packageFilter(dir string) string { return "-E '^" + dir + "/[^/]+$'" }

// dryCases are the runs of `make -n`: the coefficient by command line and by environment, the efficacy
// (two values, so that a hardcoded one fails, and the explicitly empty one) by command line and by
// environment, the extended run whatever the efficacy says, BASE, WORKERS, the PKG filter, and the lint
// target that runs this check.
func dryCases() []dryCase {
	cases := make([]dryCase, 0, 40)
	targets := []string{targetMutation, targetDiff, targetExtended}
	for _, coefficient := range []string{defaultCoeff, otherCoeff} {
		for _, target := range targets {
			p := gremlinsPins(target, coefficient)
			want := recipeOf(target, nil, coefficient)
			cases = append(cases,
				dryCase{label: fmt.Sprintf("%s with the coefficient %s on the command line", target, coefficient),
					args: []string{target, "TIMEOUT_COEFFICIENT=" + coefficient}, want: want, pins: p, target: target},
				dryCase{label: fmt.Sprintf("%s with the coefficient %s in the environment", target, coefficient),
					args: []string{target}, env: []string{"TIMEOUT_COEFFICIENT=" + coefficient}, want: want, pins: p, target: target})
		}
	}
	for _, value := range []string{"90", "85.5", ""} {
		for _, target := range []string{targetMutation, targetDiff} {
			efficacy := value
			cases = append(cases, dryCase{label: fmt.Sprintf("%s with EFFICACY=%q", target, value),
				args: []string{target, "EFFICACY=" + value}, want: recipeOf(target, &efficacy, defaultCoeff),
				pins: gremlinsPins(target, defaultCoeff), target: target})
		}
	}
	ninety := "90"
	for _, target := range []string{targetMutation, targetDiff} {
		cases = append(cases, dryCase{label: target + " with EFFICACY=90 in the environment", args: []string{target},
			env: []string{"EFFICACY=90"}, want: recipeOf(target, &ninety, defaultCoeff),
			pins: gremlinsPins(target, defaultCoeff), target: target})
	}
	cases = append(cases,
		dryCase{label: "mutation-extended with EFFICACY=90 (informational: no threshold, no check)",
			args: []string{targetExtended, "EFFICACY=90"}, want: recipeOf(targetExtended, nil, defaultCoeff),
			pins: gremlinsPins(targetExtended, defaultCoeff), target: targetExtended},
		dryCase{label: "mutation-diff with BASE=v1.2.3", args: []string{targetDiff, "BASE=v1.2.3"},
			pins:   pins{ending: " --output " + reportFile + " --diff v1.2.3", has: []string{"--timeout-coefficient " + defaultCoeff}},
			target: targetDiff},
	)
	cases = append(cases, filterCases()...)
	cases = append(cases, dryCase{label: "recipemutants runs the mutants test with its tag and needs its PASS line",
		args: []string{mutantsTarget}, target: mutantsTarget, plain: true, want: mutantsRecipe})
	cases = append(cases, dryCase{label: "make zizmor runs the script with the number the Makefile holds",
		args: []string{zizmorRunTarget, "ZIZMOR_SELF_REPOSITORY_FINDINGS=" + zizmorCount}, target: zizmorRunTarget, plain: true, looseTabs: true, want: zizmorRunRecipe})
	cases = append(cases, dryCase{label: "make lint runs this check, the dead-code check and the zizmor probes",
		args: []string{"lint", "ZIZMOR_SELF_REPOSITORY_FINDINGS=" + zizmorCount}, target: "lint",
		pins: pins{has: append(slices.Clone(deadcodeRecipe), zizmorRecipe)}})
	return cases
}

// filterCases are the runs for the words of the gremlins line that the targets share: --workers
// (WORKERS, default 4) and the package filter (PKG).
func filterCases() []dryCase {
	workers := func(n string) []string { return []string{"--workers " + n} }
	withPKG := gremlinsPins(targetMutation, defaultCoeff, "--workers 4", packageFilter("safety"), packageFilter("internal/udp"))
	withPKG.lacks = []string{packageFilter("model")}
	noPKG := gremlinsPins(targetMutation, defaultCoeff, workers("4")...)
	noPKG.lacks = []string{packageFilter("safety")}
	diffAtRoot := gremlinsPins(targetDiff, defaultCoeff, packageFilter("model"))
	return []dryCase{
		{label: "mutation with the default workers and no PKG", args: []string{targetMutation}, pins: noPKG, target: targetMutation},
		{label: "mutation with WORKERS=7", args: []string{targetMutation, "WORKERS=7"},
			pins: gremlinsPins(targetMutation, defaultCoeff, workers("7")...), target: targetMutation},
		{label: "mutation with PKG=model", args: []string{targetMutation, "PKG=model"}, pins: withPKG, target: targetMutation},
		{label: "mutation-diff with PKG=.", args: []string{targetDiff, "PKG=."}, pins: diffAtRoot, target: targetDiff},
	}
}

// mutantsRecipe is the recipe of `make recipemutants` as `make -n` prints it: the test is run with the
// mutants tag and its output kept, and the log must have the PASS line of the test, because `go test`
// exits 0 with "no tests to run" when a tag or a name is wrong.
var mutantsRecipe = []string{
	"mkdir -p build",
	"go -C devtools test -tags mutants -count=1 -v -timeout 15m -run '^TestMakefileMutants$' ./recipecheck > build/recipemutants.log || { cat build/recipemutants.log; exit 1; }",
	"grep -e '^--- PASS: TestMakefileMutants ' build/recipemutants.log",
}

// mutantsSource is the same recipe as it is written in the Makefile, where `make -n` hides a prefix.
var mutantsSource = []string{
	"\t@mkdir -p build",
	"\tgo -C devtools test -tags mutants -count=1 -v -timeout 15m -run '^TestMakefileMutants$$' ./recipecheck > build/recipemutants.log || { cat build/recipemutants.log; exit 1; }",
	"\tgrep -e '^--- PASS: TestMakefileMutants ' build/recipemutants.log",
}

// expectation is what an exit-code run must do.
type expectation int

const (
	// succeeds: make exits 0.
	succeeds expectation = iota
	// fails: make exits non-zero.
	fails
	// refuses: make exits non-zero and gremlins was never started, because what is wrong with the
	// arguments is found before the run.
	refuses
)

// The reports the stand-in leaves where gremlins leaves its own, or none.
const (
	noReport   = ""
	goodReport = "good"
	badReport  = "bad"
)

// exitCase is one run of make with a stand-in for gremlins: the stand-in writes the report named by
// report (or none), stale puts the good report in build/ before the run (so that a recipe that does not
// delete it judges it), and make must do want.
type exitCase struct {
	label  string
	args   []string
	report string
	stale  bool
	want   expectation
}

// badCoefficients are the values the coefficient validation must refuse before gremlins starts: not
// positive, not a number, not whole, not in the plain form gremlins and the check read the same way
// (`010` is octal 8 to gremlins), out of the range that keeps its budget from overflowing.
var badCoefficients = []string{"-1", "0", "abc", "1.5", "", "010", "10000"}

// exitCases are the runs of make with a stand-in for gremlins, for each target that judges a report:
// a good, a bad, a missing and a stale report, thresholds that must be refused, coefficients that must
// be refused; and for the extended run the coefficients, which are checked there too (`make -n` hides a
// `-` prefix, a failing step that does not fail make, so the exit codes are run).
func exitCases() []exitCase {
	var cases []exitCase
	for _, target := range []string{targetMutation, targetDiff} {
		add := func(label string, report string, stale bool, want expectation, args ...string) {
			cases = append(cases, exitCase{label: target + " " + label, args: append([]string{target}, args...),
				report: report, stale: stale, want: want})
		}
		add("with a good report", goodReport, false, succeeds, "EFFICACY=90")
		add("with a report below the threshold", badReport, false, fails, "EFFICACY=90")
		add("with a report below a threshold of 50", badReport, false, succeeds, "EFFICACY=50")
		add("without a report", noReport, false, fails, "EFFICACY=90")
		add("with a stale good report", noReport, true, fails, "EFFICACY=90")
		add("with a threshold that is not a percentage", goodReport, false, refuses, "EFFICACY=90%")
		add("with an empty threshold", goodReport, false, refuses, "EFFICACY=")
		add("with a threshold of spaces", goodReport, false, refuses, "EFFICACY= ")
		add("without a threshold", goodReport, false, succeeds)
		add("with a positive coefficient", goodReport, false, succeeds, "TIMEOUT_COEFFICIENT=7")
		for _, coefficient := range badCoefficients {
			add(fmt.Sprintf("with the coefficient %q", coefficient), goodReport, false, refuses, "TIMEOUT_COEFFICIENT="+coefficient)
		}
	}
	for _, coefficient := range badCoefficients {
		cases = append(cases, exitCase{label: fmt.Sprintf("%s with the coefficient %q", targetExtended, coefficient),
			args: []string{targetExtended, "TIMEOUT_COEFFICIENT=" + coefficient}, report: noReport, want: refuses})
	}
	cases = append(cases, exitCase{label: targetExtended + " with the default coefficient",
		args: []string{targetExtended}, report: noReport, want: succeeds})
	return cases
}

// describeLines joins lines for a message.
func describeLines(lines []string) string { return strings.Join(lines, "\n") }
