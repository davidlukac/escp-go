package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// gitFunc runs git in the repository and returns its standard output. The error carries git's own
// message.
type gitFunc func(args ...string) (string, error)

// systemGit runs the real git in dir.
func systemGit(dir string) gitFunc {
	return func(args ...string) (string, error) {
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // git with arguments built by this program
		cmd.Env = withoutGitVariables(os.Environ())
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
		}
		return stdout.String(), nil
	}
}

// withoutGitVariables drops the GIT_ variables from an environment. A hook sets GIT_DIR and
// GIT_INDEX_FILE for the repository it runs in, and they would send `git -C dir` to that repository
// instead of dir: the command, and the tests that run it from the pre-commit hook, must read the
// repository they are given.
func withoutGitVariables(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, "GIT_") {
			out = append(out, kv)
		}
	}
	return out
}

// revRange is a range of the history, by commit id.
type revRange struct {
	fromSHA, toSHA string
	commits        int
}

// resolveRange checks that both names are commits, that the range is not empty and runs forward, and
// that a range that ends at the checked-out commit has no uncommitted change behind it.
func resolveRange(g gitFunc, from, to string) (revRange, error) {
	var r revRange
	var err error
	if r.fromSHA, err = commitOf(g, from); err != nil {
		return r, err
	}
	if r.toSHA, err = commitOf(g, to); err != nil {
		return r, err
	}
	if r.fromSHA == r.toSHA {
		return r, fmt.Errorf("%s and %s are the same commit: the range is empty", from, to)
	}
	if _, err := g("merge-base", "--is-ancestor", r.fromSHA, r.toSHA); err != nil {
		return r, fmt.Errorf("%s is not an ancestor of %s: name the older revision first", from, to)
	}
	if err := cleanIfHead(g, r.toSHA, to); err != nil {
		return r, err
	}
	count, err := g("rev-list", "--count", r.fromSHA+".."+r.toSHA)
	if err != nil {
		return r, err
	}
	if r.commits, err = strconv.Atoi(strings.TrimSpace(count)); err != nil {
		return r, fmt.Errorf("git rev-list --count: %w", err)
	}
	return r, nil
}

// commitOf is the commit a name stands for.
func commitOf(g gitFunc, name string) (string, error) {
	out, err := g("rev-parse", "--verify", "--quiet", name+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("unknown revision %q (a tag, a branch or a commit of this repository)", name)
	}
	return strings.TrimSpace(out), nil
}

// cleanIfHead refuses a tracked change in the working tree when sha is the checked-out commit: the
// listing is of committed code, and the change would not be in it.
func cleanIfHead(g gitFunc, sha, name string) error {
	head, err := g("rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if strings.TrimSpace(head) != sha {
		return nil
	}
	status, err := g("status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("%s is the checked-out commit and the working tree has uncommitted changes: commit or stash them first", name)
	}
	return nil
}

// loadTree reads the declarations of every production Go file of a commit.
func loadTree(g gitFunc, sha string) (*tree, error) {
	goMod, err := g("show", sha+":go.mod")
	if err != nil {
		return nil, errors.New("no go.mod: not a Go module")
	}
	t := &tree{nodes: map[string]*node{}, pkgNames: map[string]string{}}
	if t.module = moduleOf(goMod); t.module == "" {
		return nil, errors.New("go.mod has no module line")
	}
	names, err := g("ls-tree", "-r", "-z", "--name-only", sha)
	if err != nil {
		return nil, err
	}
	files := strings.Split(names, "\x00")
	sort.Strings(files)
	for _, f := range files {
		if !isSurfaceFile(f) {
			continue
		}
		src, err := g("show", sha+":"+f)
		if err != nil {
			return nil, err
		}
		if err := t.addFile(f, []byte(src)); err != nil {
			return nil, err
		}
	}
	return t, nil
}

// moduleOf is the module path of a go.mod.
func moduleOf(goMod string) string {
	for line := range strings.SplitSeq(goMod, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`)
		}
	}
	return ""
}

// fileCount is the number of commits of the range that touched a production file.
type fileCount struct {
	file    string
	commits int
}

// commitCounts counts, for each production Go file, the commits of the range that touched it, and keeps
// the files that more than one commit touched, the most touched first.
func commitCounts(g gitFunc, r revRange) ([]fileCount, error) {
	out, err := g("log", "--name-only", "--format=%x00", "--no-renames", r.fromSHA+".."+r.toSHA)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for commit := range strings.SplitSeq(out, "\x00") {
		seen := map[string]bool{}
		for name := range strings.SplitSeq(commit, "\n") {
			if isSurfaceFile(name) && !seen[name] {
				seen[name] = true
				counts[name]++
			}
		}
	}
	var many []fileCount
	for f, n := range counts {
		if n > 1 {
			many = append(many, fileCount{f, n})
		}
	}
	sort.Slice(many, func(i, j int) bool {
		if many[i].commits != many[j].commits {
			return many[i].commits > many[j].commits
		}
		return many[i].file < many[j].file
	})
	return many, nil
}
