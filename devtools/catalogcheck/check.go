package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	idUse      = regexp.MustCompile(`\bDC-[CL][0-9]{2}\b`)
	catalogRef = regexp.MustCompile(`defect-classes\.md#([A-Za-z0-9_-]+)`)
	// textFile is what is searched for ids and links: documentation, code, workflows and scripts.
	textFile = regexp.MustCompile(`(\.(md|go|yml|yaml|sh|txt)|(^|/)Makefile)$`)
)

// Check reads the catalog of the repository at root and everything it names, and returns the
// problems, sorted by file and line. An error means the repository could not be read at all.
func Check(root string) ([]Problem, error) {
	text, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(catalogPath)))
	if err != nil {
		return nil, fmt.Errorf("reading the catalog: %w", err)
	}
	r, err := loadRepo(root)
	if err != nil {
		return nil, err
	}
	cat := parseCatalog(string(text))
	problems := append([]Problem{}, r.problems...)
	problems = append(problems, cat.problems...)
	problems = append(problems, cat.shapeProblems()...)
	problems = append(problems, cat.routingProblems()...)
	problems = append(problems, r.automationProblems(cat)...)
	idProblems, err := r.idProblems(cat)
	if err != nil {
		return nil, err
	}
	problems = append(problems, idProblems...)
	sort.SliceStable(problems, func(i, j int) bool {
		if problems[i].File != problems[j].File {
			return problems[i].File < problems[j].File
		}
		return problems[i].Line < problems[j].Line
	})
	return problems, nil
}

// automationProblems resolves the tokens of the Automation field of every entry.
func (r *repo) automationProblems(cat catalog) []Problem {
	var out []Problem
	for _, e := range cat.entries {
		for _, f := range e.fields {
			if f.label != automationField {
				continue
			}
			tokens, bad := parseTokens(f.text)
			for _, b := range bad {
				out = append(out, Problem{catalogPath, f.line, e.id + ": " + b})
			}
			if len(tokens) == 0 {
				out = append(out, Problem{catalogPath, f.line, e.id + ": Automation names no check (a claim without a name is not accepted: use `reading:` with a reason or `proposed:` with an issue)"})
			}
			for _, t := range tokens {
				for _, msg := range r.tokenProblems(t) {
					out = append(out, Problem{catalogPath, f.line, e.id + ": " + msg})
				}
			}
		}
	}
	return out
}

// tokenProblems checks one token: its form, then that its name resolves in the repository.
func (r *repo) tokenProblems(t automationToken) []string {
	if msgs := t.formProblems(); msgs != nil {
		return msgs
	}
	if t.kind == kindProposed {
		return r.proposedProblems(t)
	}
	if t.kind == kindReading || r.resolves(t) {
		return nil
	}
	return []string{fmt.Sprintf("`%s:%s` does not resolve: %s", t.kind, t.name, r.whereToLook(t.kind))}
}

// resolves reports whether the name of a token of a kind that names something exists.
func (r *repo) resolves(t automationToken) bool {
	switch t.kind {
	case kindTest, kindFuzz:
		return r.funcs[t.name]
	case kindMake:
		return r.targets[t.name]
	case kindLint:
		return r.linters[t.name]
	case kindAnalyzer:
		return r.devDirs[t.name] || r.analyzers[t.name]
	case kindScript:
		return r.has(t.name)
	case kindWorkflow:
		return r.has(".github/workflows/" + t.name)
	}
	return false
}

// whereToLook says where a name of the kind is looked up, for the message of a name that is not there.
func (r *repo) whereToLook(kind string) string {
	switch kind {
	case kindTest:
		return "no function of that name in a _test.go file (a renamed or removed test?)"
	case kindFuzz:
		return "no fuzz target of that name in a _test.go file"
	case kindMake:
		return "no such target in the Makefile"
	case kindLint:
		if !r.lintersOK {
			return "the " + lintConfig + " has no `linters: enable:` list"
		}
		return "not enabled in " + lintConfig
	case kindAnalyzer:
		return "no directory of that name in devtools, and no analyzer of that name declared there"
	case kindScript:
		return "no such file"
	default:
		return "no such file in .github/workflows"
	}
}

// proposedProblems reports a proposal whose name already exists: it was built, and the sentence
// must name it with its kind.
func (r *repo) proposedProblems(t automationToken) []string {
	base, _, _ := strings.Cut(t.name, "(")
	if r.funcs[base] || r.targets[base] || r.devDirs[base] || r.analyzers[base] {
		return []string{fmt.Sprintf("`proposed:%s` is built already: name it with its kind (test:, fuzz:, make:, analyzer:)", t.name)}
	}
	return nil
}

// idProblems checks every `DC-xnn` id and every link `defect-classes.md#anchor` written in a text
// file of the repository: it must be an entry, or an anchor of the catalog.
func (r *repo) idProblems(cat catalog) ([]Problem, error) {
	ids := map[string]bool{}
	for _, e := range cat.entries {
		ids[e.id] = true
	}
	var out []Problem
	for _, rel := range r.files {
		if !textFile.MatchString(rel) || strings.HasPrefix(rel, devtoolsDir+"/") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(r.root, filepath.FromSlash(rel)))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", rel, err)
		}
		out = append(out, usedIDProblems(rel, string(data), ids, cat.anchors)...)
	}
	return out, nil
}

// usedIDProblems finds the ids and the catalog links of one file that resolve to nothing.
func usedIDProblems(rel, text string, ids, anchors map[string]bool) []Problem {
	var out []Problem
	for i, line := range strings.Split(text, "\n") {
		for _, id := range idUse.FindAllString(line, -1) {
			if !ids[id] {
				out = append(out, Problem{rel, i + 1, fmt.Sprintf("%s is not an entry of %s", id, catalogPath)})
			}
		}
		for _, m := range catalogRef.FindAllStringSubmatch(line, -1) {
			if !anchors[m[1]] {
				out = append(out, Problem{rel, i + 1, fmt.Sprintf("the link defect-classes.md#%s goes to no anchor of %s", m[1], catalogPath)})
			}
		}
	}
	return out
}
