package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// newChecker is a checker over a scratch repository that talks to the ideal make.
func newChecker(t *testing.T, m *idealMake) *checker {
	t.Helper()
	root := repoRoot(t)
	m.root = root
	return &checker{root: root, run: m.runner(), standin: "'standin-binary' " + standinCommand, files: osFiles{}}
}

func TestAMakefileAsPinnedHasNoProblem(t *testing.T) {
	c := newChecker(t, newIdealMake(t))
	problems, err := c.check(context.Background())
	if err != nil || len(problems) != 0 {
		t.Fatalf("problems %q, error %v", problems, err)
	}
}

// deviation breaks one answer of the ideal make for a case; it says whether it applies to the case.
type deviation struct {
	name  string
	apply func(tc dryCase, lines []string) ([]string, bool)
}

func lastNonGremlins(lines []string) int {
	for i := range slices.Backward(lines) {
		if !strings.Contains(lines[i], gremlinsMarker) {
			return i
		}
	}
	return -1
}

func gremlinsAt(lines []string) int {
	for i, line := range lines {
		if strings.Contains(line, gremlinsMarker) {
			return i
		}
	}
	return -1
}

func deviations() []deviation {
	return []deviation{
		{"a first line is missing", func(tc dryCase, lines []string) ([]string, bool) {
			return lines[1:], tc.want != nil
		}},
		{"a step made non-fatal with || true", func(tc dryCase, lines []string) ([]string, bool) {
			i := lastNonGremlins(lines)
			out := append([]string{}, lines...)
			out[i] += " || true"
			return out, tc.want != nil && tc.target != "lint"
		}},
		{"a step added", func(tc dryCase, lines []string) ([]string, bool) {
			return append(append([]string{}, lines...), "echo done"), tc.want != nil
		}},
		{"gremlins run twice", func(_ dryCase, lines []string) ([]string, bool) {
			i := gremlinsAt(lines)
			return append(append([]string{}, lines...), lines[i]), true
		}},
		{"gremlins not run", func(tc dryCase, lines []string) ([]string, bool) {
			i := gremlinsAt(lines)
			return append(append([]string{}, lines[:i]...), lines[i+1:]...), true
		}},
		{"the threshold given to gremlins", func(_ dryCase, lines []string) ([]string, bool) {
			i := gremlinsAt(lines)
			out := append([]string{}, lines...)
			out[i] += " --threshold-efficacy 90"
			return out, true
		}},
		{"the threshold given to gremlins, in the middle", func(_ dryCase, lines []string) ([]string, bool) {
			i := gremlinsAt(lines)
			out := append([]string{}, lines...)
			out[i] = strings.Replace(out[i], gremlinsPrefix, gremlinsPrefix+"--threshold-efficacy 90 ", 1)
			return out, true
		}},
		{"the tool's prefix missing", func(tc dryCase, lines []string) ([]string, bool) {
			i := gremlinsAt(lines)
			out := append([]string{}, lines...)
			out[i] = strings.TrimPrefix(out[i], "GOFLAGS=-count=1 ")
			return out, tc.want != nil
		}},
	}
}

func TestEveryRecipeCaseNoticesEachDeviation(t *testing.T) {
	for _, tc := range dryCases() {
		if tc.target == "lint" || tc.plain {
			continue // no gremlins line; the tests of those cases
		}
		for _, dev := range deviations() {
			t.Run(tc.label+": "+dev.name, func(t *testing.T) {
				m := newIdealMake(t)
				applied := false
				m.dryLines = func(got dryCase, lines []string) []string {
					if got.label != tc.label {
						return lines
					}
					out, ok := dev.apply(got, lines)
					applied = ok
					if !ok {
						return lines
					}
					return out
				}
				problems, err := newChecker(t, m).check(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, p := range problems {
					found = found || strings.HasPrefix(p, tc.label+":")
				}
				if applied && !found {
					t.Errorf("the deviation was not noticed; problems %q", problems)
				}
				if !applied && len(problems) != 0 {
					t.Errorf("a case the deviation does not apply to has problems: %q", problems)
				}
			})
		}
	}
}

func TestEveryPinnedWordOfTheGremlinsLineIsRequired(t *testing.T) {
	for _, tc := range dryCases() {
		if tc.target == "lint" {
			continue // the lines of `make lint` are not on the gremlins line; TestTheLintTargetMustPrintTheDeadcodeLinesWhole
		}
		var words []string
		words = append(words, tc.pins.has...)
		if tc.pins.ending != "" {
			words = append(words, tc.pins.ending)
		}
		for _, word := range words {
			t.Run(tc.label+": without "+word, func(t *testing.T) {
				m := newIdealMake(t)
				m.dryLines = func(got dryCase, lines []string) []string {
					if got.label != tc.label {
						return lines
					}
					// a word that the case also finds elsewhere in the line is taken out of its last place
					i := gremlinsAt(lines)
					out := append([]string{}, lines...)
					at := strings.LastIndex(out[i], word)
					out[i] = out[i][:at] + out[i][at+len(word):]
					return out
				}
				problems, err := newChecker(t, m).check(context.Background())
				if err != nil || !hasProblemFor(problems, tc.label) {
					t.Errorf("not noticed: problems %q, error %v", problems, err)
				}
			})
		}
		for _, word := range tc.pins.lacks {
			t.Run(tc.label+": with "+word, func(t *testing.T) {
				m := newIdealMake(t)
				m.dryLines = func(got dryCase, lines []string) []string {
					if got.label != tc.label {
						return lines
					}
					out := append([]string{}, lines...)
					out[gremlinsAt(out)] += " " + word
					return out
				}
				problems, err := newChecker(t, m).check(context.Background())
				if err != nil || !hasProblemFor(problems, tc.label) {
					t.Errorf("not noticed: problems %q, error %v", problems, err)
				}
			})
		}
	}
}

func hasProblemFor(problems []string, label string) bool {
	for _, p := range problems {
		if strings.HasPrefix(p, label+":") {
			return true
		}
	}
	return false
}

// The lint line must be the recipe of the mutationrecipe target with the root, whole: a prefix that
// matches is not enough (`|| true`, an extra flag), and a root resolved through symbolic links is the
// same root.
func TestTheLintTargetMustRunTheCheckAsTheWholeLine(t *testing.T) {
	tests := []struct {
		name  string
		lines func(root string) []string
		want  bool // a problem
	}{
		{"the line as it is", func(root string) []string { return []string{"go build", lintRecipe + root} }, false},
		{"the line with its links resolved", func(root string) []string {
			resolved, _ := filepath.EvalSymlinks(root)
			return []string{lintRecipe + resolved}
		}, false},
		{"another recipe", func(string) []string { return []string{"go build", "go run ./other"} }, true},
		{"the check made non-fatal", func(root string) []string { return []string{lintRecipe + root + " || true"} }, true},
		{"an extra flag", func(root string) []string { return []string{lintRecipe + root + " -make true"} }, true},
		{"another root", func(string) []string { return []string{lintRecipe + "/elsewhere"} }, true},
		{"the prefix only", func(string) []string { return []string{lintRecipe} }, true},
		{"a second root flag", func(root string) []string { return []string{lintRecipe + root + " -root /tmp"} }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newIdealMake(t)
			c := newChecker(t, m)
			m.dryLines = func(tc dryCase, lines []string) []string {
				if tc.target == "lint" {
					return append(tt.lines(c.root), deadcodeRecipe...)
				}
				return lines
			}
			problems, err := c.check(context.Background())
			if err != nil || (len(problems) == 1) != tt.want || len(problems) > 1 ||
				(tt.want && !strings.Contains(problems[0], "make lint does not run the check")) {
				t.Errorf("problems %q, error %v, want a problem: %v", problems, err, tt.want)
			}
		})
	}
}

// `make lint` must print each line of the dead-code check whole: a line gone (the prerequisite deleted,
// the run with the live tag dropped), a suffix (`|| true`), another configuration (the main one, which has
// the tests on and many linters) or another tag each leave no line that is the wanted one.
func TestTheLintTargetMustPrintTheDeadcodeLinesWhole(t *testing.T) {
	first, second := deadcodeRecipe[0], deadcodeRecipe[1]
	tests := []struct {
		name  string
		lines []string
		want  int // the number of problems
	}{
		{"both lines", []string{first, second}, 0},
		{"the prerequisite deleted", nil, 2},
		{"the first line only", []string{first}, 1},
		{"the live line only", []string{second}, 1},
		{"the first line made non-fatal", []string{first + " || true", second}, 1},
		{"the live line made non-fatal", []string{first, second + " || true"}, 1},
		{"the main configuration", []string{strings.Replace(first, ".golangci-deadcode.yml", ".golangci.yml", 1), second}, 1},
		{"no configuration", []string{strings.Replace(first, " --config .golangci-deadcode.yml", "", 1), second}, 1},
		{"another tag", []string{first, strings.Replace(second, "live", "mutants", 1)}, 1},
		{"the tests turned on by a flag", []string{first + " --tests", second}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newIdealMake(t)
			c := newChecker(t, m)
			m.dryLines = func(tc dryCase, lines []string) []string {
				if tc.target == "lint" {
					return append([]string{"go build", lintRecipe + c.root}, tt.lines...)
				}
				return lines
			}
			problems, err := c.check(context.Background())
			if err != nil || len(problems) != tt.want {
				t.Errorf("problems %q, error %v, want %d", problems, err, tt.want)
			}
			for _, p := range problems {
				if !strings.Contains(p, "make lint does not run `go tool") {
					t.Errorf("a problem that is not about a line of the dead-code check: %q", p)
				}
			}
		})
	}
}

// `make -n` hides a `-` and an `@`, so the recipe of the mutationrecipe target is also read as it is
// written in the Makefile.
func TestTheRecipeOfTheCheckTargetIsPinnedAsWritten(t *testing.T) {
	tests := []struct {
		name     string
		makefile string
		want     string // a word of the problem, "" for none
	}{
		{"as it is", goodMakefile, ""},
		{"a - prefix", strings.Replace(goodMakefile, "\tgo -C", "\t-go -C", 1), "mutationrecipe is not as written"},
		{"an @ prefix", strings.Replace(goodMakefile, "\tgo -C", "\t@go -C", 1), "mutationrecipe is not as written"},
		{"|| true", strings.Replace(goodMakefile, "$(CURDIR)", "$(CURDIR) || true", 1), "mutationrecipe is not as written"},
		{"an extra flag", strings.Replace(goodMakefile, "$(CURDIR)", "$(CURDIR) -make true", 1), "mutationrecipe is not as written"},
		{"another line", strings.Replace(goodMakefile, recipeSource+"\n", recipeSource+"\n\ttrue\n", 1), "mutationrecipe is not as written"},
		{"no recipe", strings.Replace(goodMakefile, recipeSource+"\n", "", 1), "mutationrecipe is not as written"},
		{"no target", "all:\n\ttrue\n", "no mutationrecipe target"},
		{"a - prefix on the dead-code run", strings.Replace(goodMakefile, "\t$(DEVTOOL) golangci-lint run --config .golangci-deadcode.yml ./...", "\t-$(DEVTOOL) golangci-lint run --config .golangci-deadcode.yml ./...", 1), "deadcode is not as written"},
		{"an @ prefix on the dead-code live run", strings.Replace(goodMakefile, "\t$(DEVTOOL) golangci-lint run --config .golangci-deadcode.yml --build-tags live", "\t@$(DEVTOOL) golangci-lint run --config .golangci-deadcode.yml --build-tags live", 1), "deadcode is not as written"},
		{"the dead-code live run missing", strings.Replace(goodMakefile, deadcodeSource[1]+"\n", "", 1), "deadcode is not as written"},
		{"the mutants recipe without the PASS line", strings.Replace(goodMakefile, mutantsSource[2]+"\n", "", 1), "recipemutants"},
		{"a - prefix on the PASS line", strings.Replace(goodMakefile, "\tgrep -e", "\t-grep -e", 1), "recipemutants"},
		{"an @ prefix on the test run", strings.Replace(goodMakefile, "\tgo -C devtools test", "\t@go -C devtools test", 1), "recipemutants"},
		{"the test run made non-fatal", strings.Replace(goodMakefile, "exit 1; }", "exit 1; } || true", 1), "recipemutants"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			problems := sourceProblems(tt.makefile)
			switch {
			case tt.want == "" && len(problems) != 0, tt.want != "" && !strings.Contains(strings.Join(problems, "\n"), tt.want):
				t.Errorf("problems %q, want %q", problems, tt.want)
			}
		})
	}
}

func TestAMakefileThatCannotBeReadStopsTheCheck(t *testing.T) {
	c := newChecker(t, newIdealMake(t))
	if err := os.Remove(filepath.Join(c.root, "Makefile")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.check(context.Background()); err == nil || !strings.Contains(err.Error(), "reading the Makefile") {
		t.Errorf("error %v", err)
	}
}

// A recipe without gremlins is compared whole.
func TestAPlainRecipeCaseNoticesEachDeviation(t *testing.T) {
	deviations := map[string]func(lines []string) []string{
		"a line missing": func(lines []string) []string { return lines[1:] },
		"a step made non-fatal": func(lines []string) []string {
			return append(slices.Clone(lines[:len(lines)-1]), lines[len(lines)-1]+" || true")
		},
		"a step added":         func(lines []string) []string { return append(slices.Clone(lines), "true") },
		"the log not required": func(lines []string) []string { return lines[:len(lines)-1] },
	}
	for _, tc := range dryCases() {
		if !tc.plain {
			continue
		}
		for name, deviate := range deviations {
			t.Run(tc.label+": "+name, func(t *testing.T) {
				m := newIdealMake(t)
				m.dryLines = func(got dryCase, lines []string) []string {
					if got.label == tc.label {
						return deviate(lines)
					}
					return lines
				}
				problems, err := newChecker(t, m).check(context.Background())
				if err != nil || len(problems) != 1 || !strings.HasPrefix(problems[0], tc.label+":") {
					t.Errorf("problems %q, error %v", problems, err)
				}
			})
		}
	}
}

func TestAMakeNThatFailsIsAProblem(t *testing.T) {
	m := newIdealMake(t)
	m.dryStatus = 2
	problems, err := newChecker(t, m).check(context.Background())
	if err != nil || len(problems) != len(dryCases()) || !strings.Contains(problems[0], "make -n exited with status 2") {
		t.Errorf("%d problems, first %q, error %v", len(problems), problems, err)
	}
}

func TestEveryExitCaseNoticesAWrongStatus(t *testing.T) {
	for _, tc := range exitCases() {
		flips := []struct {
			name   string
			status func(status int) int
			want   string
		}{
			{"the opposite status", func(s int) int { return 1 - s }, map[bool]string{true: "must succeed", false: "must fail"}[tc.want == succeeds]},
		}
		for _, flip := range flips {
			t.Run(tc.label+": "+flip.name, func(t *testing.T) {
				m := newIdealMake(t)
				m.exitStatus = func(got exitCase, status int, ran bool) (int, bool) {
					if got.label == tc.label {
						return flip.status(status), ran
					}
					return status, ran
				}
				problems, err := newChecker(t, m).check(context.Background())
				if err != nil || len(problems) != 1 || !strings.Contains(problems[0], tc.label+": make") || !strings.Contains(problems[0], flip.want) {
					t.Errorf("problems %q, error %v", problems, err)
				}
			})
		}
		if tc.want != refuses {
			continue
		}
		t.Run(tc.label+": refused only after gremlins started", func(t *testing.T) {
			m := newIdealMake(t)
			m.exitStatus = func(got exitCase, status int, ran bool) (int, bool) {
				return status, ran || got.label == tc.label
			}
			problems, err := newChecker(t, m).check(context.Background())
			if err != nil || len(problems) != 1 || !strings.Contains(problems[0], "only after gremlins was started") {
				t.Errorf("problems %q, error %v", problems, err)
			}
		})
	}
}

func TestTheStandInIsGivenAFreshLogAndTheReportOfTheCase(t *testing.T) {
	m := newIdealMake(t)
	reports := map[string]string{}
	m.onRun = func(env, args []string) error {
		if len(args) > 0 && args[0] == "-n" {
			return nil
		}
		var report string
		for _, entry := range env {
			if v, ok := strings.CutPrefix(entry, "STANDIN_REPORT="); ok {
				report = v
			}
		}
		for _, entry := range env {
			if v, ok := strings.CutPrefix(entry, caseVariable+"="); ok {
				reports[v] = report
			}
		}
		return nil
	}
	if problems, err := newChecker(t, m).check(context.Background()); err != nil || len(problems) != 0 {
		t.Fatalf("problems %q, error %v", problems, err)
	}
	for _, tc := range exitCases() {
		got := reports[tc.label]
		switch tc.report {
		case noReport:
			if got != "" {
				t.Errorf("%s: report %q, want none", tc.label, got)
			}
		case goodReport:
			if filepath.Base(got) != "good.json" {
				t.Errorf("%s: report %q, want the good one", tc.label, got)
			}
		case badReport:
			if filepath.Base(got) != "bad.json" {
				t.Errorf("%s: report %q, want the bad one", tc.label, got)
			}
		}
	}
}

func TestAStaleReportIsInBuildBeforeTheRunThatAsksForIt(t *testing.T) {
	m := newIdealMake(t)
	c := newChecker(t, m)
	seen := map[string]string{}
	m.onRun = func(env, args []string) error {
		if args[0] == "-n" {
			return nil
		}
		var label string
		for _, entry := range env {
			if v, ok := strings.CutPrefix(entry, caseVariable+"="); ok {
				label = v
			}
		}
		data, err := os.ReadFile(filepath.Join(c.root, "build", "mutation.json"))
		if errors.Is(err, os.ErrNotExist) {
			seen[label] = "none"
			return nil
		}
		seen[label] = string(data)
		return err
	}
	if problems, err := c.check(context.Background()); err != nil || len(problems) != 0 {
		t.Fatalf("problems %q, error %v", problems, err)
	}
	for _, tc := range exitCases() {
		got := seen[tc.label]
		if tc.stale && got != goodReportJSON {
			t.Errorf("%s: the stale report is %q", tc.label, got)
		}
		if !tc.stale && got != "none" {
			t.Errorf("%s: a report was left in build/ before the run: %q", tc.label, got)
		}
	}
}

// The report of the working tree is put aside for the exit-code runs and put back as it was, with its
// permissions, whatever happens; when there was none, a report that a run left is removed.
func TestTheReportOfTheWorkingTreeComesBack(t *testing.T) {
	const precious = `{"keep":"me"}`
	tests := []struct {
		name    string
		prepare func(t *testing.T, path string)
		onRun   func(t *testing.T, c *checker, cancel context.CancelFunc) func(env, args []string) error
		wantErr bool
	}{
		{name: "a run to the end", prepare: write(precious, 0o640)},
		{name: "permissions a umask would change", prepare: write(precious, 0o666)},
		{name: "none before, a stand-in's report left behind", prepare: func(*testing.T, string) {}},
		{name: "an error in the middle", prepare: write(precious, 0o640),
			onRun: func(*testing.T, *checker, context.CancelFunc) func(env, args []string) error {
				calls := 0
				return func(_, args []string) error {
					if args[0] != "-n" {
						calls++
					}
					if calls == 3 {
						return errors.New("make cannot run")
					}
					return nil
				}
			}, wantErr: true},
		{name: "a canceled context", prepare: write(precious, 0o640),
			onRun: func(_ *testing.T, _ *checker, cancel context.CancelFunc) func(env, args []string) error {
				calls := 0
				return func(_, args []string) error {
					if args[0] != "-n" {
						calls++
					}
					if calls == 2 {
						cancel()
					}
					return nil
				}
			}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newIdealMake(t)
			c := newChecker(t, m)
			path := filepath.Join(c.root, "build", "mutation.json")
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				t.Fatal(err)
			}
			tt.prepare(t, path)
			before, statErr := os.Stat(path)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tt.onRun != nil {
				m.onRun = tt.onRun(t, c, cancel)
			}
			_, err := c.check(ctx)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error %v, want error %v", err, tt.wantErr)
			}
			after, afterErr := os.Stat(path)
			if statErr != nil {
				if afterErr == nil {
					t.Fatalf("a report was left behind")
				}
				return
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != precious || after.Mode().Perm() != before.Mode().Perm() {
				t.Errorf("report %q (%v), mode %v, want %q and %v", data, err, after.Mode().Perm(), precious, before.Mode().Perm())
			}
		})
	}
}

// write makes the working tree's report with the content and permissions.
func write(content string, mode os.FileMode) func(*testing.T, string) {
	return func(t *testing.T, path string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
}

// The scratch directory is removed, and a report left by the stand-in when there was none is too.
func TestNothingIsLeftBehind(t *testing.T) {
	m := newIdealMake(t)
	c := newChecker(t, m)
	var scratch string
	m.onRun = func(env, args []string) error {
		for _, entry := range env {
			if log, ok := strings.CutPrefix(entry, "STANDIN_LOG="); ok {
				scratch = filepath.Dir(log)
			}
		}
		if len(args) > 0 && args[0] != "-n" {
			return os.WriteFile(filepath.Join(c.root, "build", "mutation.json"), []byte("{}"), 0o600)
		}
		return nil
	}
	if _, err := c.check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(scratch); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the scratch directory %s is still there (%v)", scratch, err)
	}
	if _, err := os.Stat(filepath.Join(c.root, "build", "mutation.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a report is left in build/ (%v)", err)
	}
}

func TestAMakeThatCannotRunStopsTheCheck(t *testing.T) {
	boom := errors.New("no make")
	tests := []struct {
		name string
		at   func(args []string) bool
	}{
		{"in a recipe run", func(args []string) bool { return args[0] == "-n" }},
		{"in an exit-code run", func(args []string) bool { return args[0] != "-n" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newIdealMake(t)
			m.onRun = func(_, args []string) error {
				if tt.at(args) {
					return boom
				}
				return nil
			}
			_, err := newChecker(t, m).check(context.Background())
			if !errors.Is(err, boom) {
				t.Errorf("error %v", err)
			}
		})
	}
}

// The file system failing at each step: the check stops with an error naming the step, and puts back
// what it moved.
func TestAFileSystemThatFailsStopsTheCheck(t *testing.T) {
	boom := errors.New("disk")
	tests := []struct {
		name string
		fail func(c *checker) func(op, name string) error
		want string
		// reportFirst: the working tree has a report before the run
		reportFirst bool
	}{
		{"no scratch directory", func(*checker) func(op, name string) error {
			return func(op, _ string) error { return only(op == "MkdirTemp", boom) }
		}, "scratch directory", false},
		{"a report cannot be written", func(*checker) func(op, name string) error {
			return func(op, name string) error {
				return only(op == "WriteFile" && strings.HasSuffix(name, "bad.json"), boom)
			}
		}, "writing a report", false},
		{"the report of the tree cannot be looked at", func(*checker) func(op, name string) error {
			return func(op, name string) error {
				return only(op == "Stat" && strings.HasSuffix(name, "mutation.json"), boom)
			}
		}, "looking at", false},
		{"the report of the tree cannot be read", func(*checker) func(op, name string) error {
			return func(op, name string) error {
				return only(op == "ReadFile" && strings.HasSuffix(name, "mutation.json"), boom)
			}
		}, "reading build/mutation.json", true},
		{"build cannot be made for a run", func(*checker) func(op, name string) error {
			return func(op, _ string) error { return only(op == "MkdirAll", boom) }
		}, "build/", false},
		{"a report cannot be removed", func(*checker) func(op, name string) error {
			return func(op, name string) error {
				return only(op == "Remove" && strings.HasSuffix(name, "mutation.json"), boom)
			}
		}, "removing", false},
		{"a stale report cannot be written", func(*checker) func(op, name string) error {
			return func(op, name string) error {
				return only(op == "WriteFile" && strings.HasSuffix(name, "mutation.json") && !strings.Contains(name, "recipecheck-"), boom)
			}
		}, "stale", false},
		{"the log cannot be emptied", func(*checker) func(op, name string) error {
			return func(op, name string) error {
				return only(op == "WriteFile" && strings.HasSuffix(name, "standin.log"), boom)
			}
		}, "boom", false},
		{"the log cannot be read", func(*checker) func(op, name string) error {
			return func(op, name string) error {
				return only(op == "ReadFile" && strings.HasSuffix(name, "standin.log"), boom)
			}
		}, "stand-in's log", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newChecker(t, newIdealMake(t))
			var temps []string
			c.files = failingFiles{fail: tt.fail(c), temps: &temps}
			if tt.reportFirst {
				if err := os.MkdirAll(filepath.Join(c.root, "build"), 0o750); err != nil {
					t.Fatal(err)
				}
				write(`{"keep":"me"}`, 0o600)(t, filepath.Join(c.root, "build", "mutation.json"))
			}
			_, err := c.check(context.Background())
			if err == nil || !errors.Is(err, boom) && !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %v, want one about %q", err, tt.want)
			}
			for _, dir := range temps {
				if _, statErr := os.Stat(dir); !errors.Is(statErr, os.ErrNotExist) {
					t.Errorf("the scratch directory %s was not removed (%v)", dir, statErr)
				}
			}
			if tt.reportFirst {
				data, readErr := os.ReadFile(filepath.Join(c.root, "build", "mutation.json"))
				if readErr != nil || string(data) != `{"keep":"me"}` {
					t.Errorf("the report of the tree is %q (%v)", data, readErr)
				}
			}
		})
	}
}

// only is err when the condition holds.
func only(condition bool, err error) error {
	if condition {
		return err
	}
	return nil
}

func TestTheCasesAreWhatTheJobsUse(t *testing.T) {
	labels := map[string]bool{}
	for _, tc := range dryCases() {
		if labels[tc.label] {
			t.Errorf("two recipe cases are called %q", tc.label)
		}
		labels[tc.label] = true
	}
	for _, tc := range exitCases() {
		if labels[tc.label] {
			t.Errorf("two cases are called %q", tc.label)
		}
		labels[tc.label] = true
	}
	if got := fmt.Sprint(recipeOf(targetExtended, nil, "10")); got != "[mkdir -p build go run ./internal/mutationreport coefficient \"10\" GREMLINS]" {
		t.Errorf("the extended recipe is %s", got)
	}
}

// The ending is a suffix of the gremlins line (a word after it, or its last word glued to another, is
// not), and a pinned word is a whole word (the same words with a digit glued on are not).
func TestThePinnedEndingIsASuffixAndThePinnedWordsAreWholeWords(t *testing.T) {
	for _, tc := range dryCases() {
		if tc.target == "lint" || (tc.pins.ending == "" && len(tc.pins.has) == 0) {
			continue // `make lint` has no gremlins line; its lines are held whole by TestTheLintTargetMustPrintTheDeadcodeLinesWhole
		}
		edits := map[string]func(line string) string{
			"a word after the line": func(line string) string { return line + " --verbose" },
			"the last word glued":   func(line string) string { return line + "x" },
		}
		for _, word := range tc.pins.has {
			edits["the word "+word+" glued to a digit"] = func(line string) string {
				at := strings.LastIndex(line, word)
				return line[:at+len(word)] + "0" + line[at+len(word):]
			}
		}
		for name, edit := range edits {
			if tc.pins.ending == "" && strings.HasPrefix(name, "a word") || tc.pins.ending == "" && strings.HasPrefix(name, "the last") {
				continue
			}
			t.Run(tc.label+": "+name, func(t *testing.T) {
				m := newIdealMake(t)
				m.dryLines = func(got dryCase, lines []string) []string {
					if got.label != tc.label {
						return lines
					}
					out := slices.Clone(lines)
					i := gremlinsAt(out)
					out[i] = edit(out[i])
					return out
				}
				problems, err := newChecker(t, m).check(context.Background())
				if err != nil || !hasProblemFor(problems, tc.label) {
					t.Errorf("not noticed: problems %q, error %v", problems, err)
				}
			})
		}
	}
}

// The reports the stand-in leaves are what they are named for: the good one passes any threshold up to
// 100 (a timed-out mutant is a kill), the bad one has a survivor.
func TestTheReportsAreAsNamed(t *testing.T) {
	for _, tt := range []struct {
		name, report string
		has, lacks   string
	}{
		{"good", goodReportJSON, `"TIMED OUT"`, `"LIVED"`},
		{"bad", badReportJSON, `"LIVED"`, `"TIMED OUT"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.report, `"KILLED"`) || !strings.Contains(tt.report, tt.has) || strings.Contains(tt.report, tt.lacks) {
				t.Errorf("the %s report is %s", tt.name, tt.report)
			}
		})
	}
}
