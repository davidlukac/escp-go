//go:build !windows

package probe

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestWithoutGitVariables(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  []string
		want []string
	}{
		{name: "what a hook exports", env: []string{"PATH=/bin", "GIT_DIR=/x/.git", "GIT_INDEX_FILE=/x/index", "GIT_WORK_TREE=/x", "HOME=/h"}, want: []string{"PATH=/bin", "HOME=/h"}},
		{name: "every name that starts with GIT_", env: []string{"GIT_COMMON_DIR=/c", "GIT_OBJECT_DIRECTORY=/o", "GIT_NAMESPACE=n", "GIT_CONFIG_COUNT=0", "GIT_=x", "A=1"}, want: []string{"A=1"}},
		{name: "a name that only contains or resembles it stays", env: []string{"GITHUB_TOKEN=t", "NOT_GIT_DIR=1", "git_dir=lower", "XGIT_DIR=2"}, want: []string{"GITHUB_TOKEN=t", "NOT_GIT_DIR=1", "git_dir=lower", "XGIT_DIR=2"}},
		{name: "a value that mentions it stays", env: []string{"A=GIT_DIR=/x", "B==GIT_DIR"}, want: []string{"A=GIT_DIR=/x", "B==GIT_DIR"}},
		{name: "an entry without a value", env: []string{"GIT_DIR", "KEPT"}, want: []string{"KEPT"}},
		{name: "the order and the repeats are kept", env: []string{"B=2", "GIT_DIR=/x", "A=1", "B=3"}, want: []string{"B=2", "A=1", "B=3"}},
		{name: "nothing but git variables", env: []string{"GIT_DIR=/x"}, want: []string{}},
		{name: "no environment", env: nil, want: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := withoutGitVariables(tc.env)
			if got == nil || !slices.Equal(got, tc.want) {
				t.Errorf("withoutGitVariables(%q) = %q (nil: %t), want %q", tc.env, got, got == nil, tc.want)
			}
		})
	}
}

// The environment handed to git by the harness holds no GIT_ variable, and git is told the directory with -C.
func TestFreshRunsGitWithoutTheGitEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  []string
		want []string
	}{
		{name: "a hook environment", env: []string{"KEPT=1", "GIT_DIR=/x/.git", "GIT_INDEX_FILE=/x/index", "GIT_WORK_TREE=/x", "PATH=/bin"}, want: []string{"KEPT=1", "PATH=/bin"}},
		{name: "an environment without git variables", env: []string{"KEPT=1"}, want: []string{"KEPT=1"}},
		{name: "an environment of git variables only", env: []string{"GIT_DIR=/x/.git"}, want: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeRunner{}
			env, _, _ := testEnvironment(t, nil, f)
			env.Environ = func() []string { return tc.env }
			h, err := newHarness(env, repoRoot, "13", "/bin/probe")
			if err != nil {
				t.Fatal(err)
			}
			defer h.close()
			tr, err := h.fresh(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(f.calls) != 1 {
				t.Fatalf("%d calls, want one", len(f.calls))
			}
			c := f.calls[0]
			if c.name != gitCommand || !slices.Equal(c.args, []string{"-C", tr.dir, "init", "-q"}) {
				t.Errorf("ran %s %q, want git -C %s init -q", c.name, c.args, tr.dir)
			}
			if c.env == nil || !slices.Equal(c.env, tc.want) {
				t.Errorf("environment %q (nil: %t), want %q", c.env, c.env == nil, tc.want)
			}
		})
	}
}

// isolateGitEnvironment removes every GIT_ variable and the configuration files of the user from the
// environment of the test process, restoring them when the test ends.
func isolateGitEnvironment(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		if name, _, _ := strings.Cut(entry, "="); strings.HasPrefix(name, gitVariablePrefix) {
			t.Setenv(name, "") // registers the restore
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

// gitIn runs the real git in dir with only the environment the test gives and returns what it printed.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@example.test",
		"-c", "commit.gpgsign=false", "-c", "core.hooksPath=" + os.DevNull}, args...)...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %q: %v\n%s", args, err, &out)
	}
	return strings.TrimSpace(out.String())
}

// A pre-commit hook in a linked worktree has GIT_DIR (and more) naming the worktree's git directory, and the
// configuration of that repository is shared with the main clone: the scratch repository of a probe must be
// made in its own directory and leave both alone. The environment is the process's own, as a hook's is.
func TestFreshLeavesTheRepositoryOfAHookAlone(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	for _, tc := range []struct {
		name string
		vars func(gitDir, workTree string) map[string]string
	}{
		{name: "GIT_DIR", vars: func(gitDir, _ string) map[string]string { return map[string]string{"GIT_DIR": gitDir} }},
		{name: "GIT_DIR and GIT_INDEX_FILE", vars: func(gitDir, _ string) map[string]string {
			return map[string]string{"GIT_DIR": gitDir, "GIT_INDEX_FILE": filepath.Join(gitDir, "index")}
		}},
		{name: "GIT_DIR, GIT_INDEX_FILE and GIT_WORK_TREE", vars: func(gitDir, workTree string) map[string]string {
			return map[string]string{"GIT_DIR": gitDir, "GIT_INDEX_FILE": filepath.Join(gitDir, "index"), "GIT_WORK_TREE": workTree}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateGitEnvironment(t)
			main := filepath.Join(t.TempDir(), "main")
			worktree := filepath.Join(t.TempDir(), "worktree")
			gitIn(t, t.TempDir(), "init", "-q", "-b", "main", main)
			gitIn(t, main, "commit", "-q", "--allow-empty", "-m", "first")
			gitIn(t, main, "worktree", "add", "-q", "-b", "topic", worktree)
			gitDir := gitIn(t, worktree, "rev-parse", "--absolute-git-dir")
			sharedConfig := filepath.Join(main, ".git", "config")
			before, err := os.ReadFile(sharedConfig)
			if err != nil {
				t.Fatal(err)
			}
			if bare := gitIn(t, main, "config", "core.bare"); bare != "false" {
				t.Fatalf("setup: core.bare = %q", bare)
			}
			for name, value := range tc.vars(gitDir, worktree) {
				t.Setenv(name, value)
			}

			h, err := newHarness(ProcessEnvironment(), repoRoot, "13", "/bin/probe")
			if err != nil {
				t.Fatal(err)
			}
			defer h.close()
			tr, err := h.fresh(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			after, err := os.ReadFile(sharedConfig)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Errorf("the configuration of the hook's repository changed:\n%s\nbecame:\n%s", before, after)
			}
			if bare := gitIn(t, main, "config", "core.bare"); bare != "false" {
				t.Errorf("core.bare of the main clone = %q, want false", bare)
			}
			if _, err := os.Stat(filepath.Join(tr.dir, ".git")); err != nil {
				t.Errorf("the scratch tree is not a repository of its own: %v", err)
			}
		})
	}
}
