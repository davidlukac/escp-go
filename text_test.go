package escp

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// textFiles returns the repository's text files that carry prose or comments: Go, Markdown, YAML,
// JSON, shell and the Makefile. History that quotes the past (the changelog, the Contributor Covenant
// text) is not checked, and neither are the checks themselves, which name what they look for.
// What is checked is what is published: the files git tracks. A file a developer keeps beside them and
// git ignores (a local editor or assistant setting) is not, so it cannot fail the test on one machine only.
func textFiles(t *testing.T) []string {
	t.Helper()
	skipDirs := map[string]bool{".git": true, "build": true, "devtools": true}
	skipFiles := map[string]bool{"CHANGELOG.md": true, "CODE_OF_CONDUCT.md": true, "text_test.go": true, "go.sum": true}
	var files []string
	for _, path := range publishedFiles(t) {
		inSkipped := false
		for _, dir := range strings.Split(filepath.Dir(path), string(filepath.Separator)) {
			inSkipped = inSkipped || skipDirs[dir]
		}
		if inSkipped || skipFiles[filepath.Base(path)] {
			continue
		}
		switch ext := filepath.Ext(path); {
		case ext == ".go" || ext == ".md" || ext == ".yml" || ext == ".json" || ext == ".sh", filepath.Base(path) == "Makefile":
			files = append(files, path)
		}
	}
	if len(files) < 50 {
		t.Fatalf("found only %d text files: the list of files is broken", len(files))
	}
	return files
}

// publishedFiles lists the files git tracks. Without git (the module's sources unpacked from an archive)
// it walks the tree, where nothing is ignored.
func publishedFiles(t *testing.T) []string {
	t.Helper()
	if out, err := exec.Command("git", "ls-files", "-z").Output(); err == nil {
		var files []string
		for _, path := range strings.Split(string(out), "\x00") {
			if path != "" {
				files = append(files, filepath.FromSlash(path))
			}
		}
		return files
	}
	var files []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// grep reports every line of the text files that matches pattern, as file:line: text.
func grep(t *testing.T, pattern *regexp.Regexp) []string {
	t.Helper()
	var hits []string
	for _, path := range textFiles(t) {
		text, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(text), "\n") {
			if pattern.MatchString(line) {
				hits = append(hits, path+":"+strconv.Itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
	}
	return hits
}

// The text is American English, as the Go standard library and godoc write it. The misspell linter
// checks the Go files; this checks the Markdown, the YAML and the rest too.
func TestTheTextIsAmericanEnglish(t *testing.T) {
	//nolint:misspell // the spellings this test forbids
	british := regexp.MustCompile(`(?i)\b(behaviours?|misbehaviours?|colours?|neighbours?|catalogues?|cancell(ed|ing)|summaris\w+|initialis\w+|serialis\w+|normalis\w+|optimis\w+|recognis\w+|organis\w+|modell(ed|ing)|honour\w*|judgement|licence|artefacts?|dialled|dialling|favour\w*|centre|whilst|amongst)\b`)
	for _, hit := range grep(t, british) {
		t.Errorf("British spelling (write American English): %s", hit)
	}
}

// What belongs to the maintainer's own workspace (switch names, private design notes, an address of a
// private network, the instructions for the assistant) does not appear in a public library.
func TestNothingPrivateToAWorkspaceIsPublished(t *testing.T) {
	private := regexp.MustCompile(`(?i)\bswitch 0[0-9]\b|local-infra|\bADR-\d{4}\b|\b192\.168\.77\.|\bthe owner\b|\bCLAUDE\.md\b|inventory\.yaml|ipMacBindings`)
	for _, hit := range grep(t, private) {
		t.Errorf("a reference to the maintainer's workspace: %s", hit)
	}
}
