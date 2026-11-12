package main

import (
	"sort"
	"strings"
	"testing"
)

// A promise one package states is often kept by the code of another (the write gate of package model is
// run by the client of the root package), and the test that pins it lives there. The test names the
// package of the identifier in front of it: `// guarantee: model.Gate`.
func TestAMarkerThatNamesAnotherPackageIsThatPackagesMarker(t *testing.T) {
	const (
		model = "package model\n\n// Gate never blocks.\ntype Gate func()\n\n// Group is never empty.\ntype Group struct{}\n"
		wire  = "package wire\n\n// Gate never blocks.\ntype Gate func()\n"
	)
	tests := []struct {
		name          string
		files         map[string]string
		wantMatched   string
		wantUnmatched string
		wantStale     string
	}{
		{name: "no marker", files: map[string]string{"model/model.go": model},
			wantUnmatched: "Gate,Group"},
		{name: "a test of the root package names a package below it", files: map[string]string{
			"model/model.go": model, "p_test.go": "package p\n\n// guarantee: model.Gate\nfunc TestGate() {}\n"},
			wantMatched: "Gate", wantUnmatched: "Group"},
		{name: "several names, each with its package", files: map[string]string{
			"model/model.go": model, "p_test.go": "package p\n\n// guarantee: model.Gate, model.Group\nfunc TestBoth() {}\n"},
			wantMatched: "Gate,Group"},
		{name: "the last element of a directory is its address", files: map[string]string{
			"internal/model/model.go": model, "p_test.go": "package p\n\n// guarantee: model.Gate\nfunc TestGate() {}\n"},
			wantMatched: "Gate", wantUnmatched: "Group"},
		{name: "a name the package does not have is stale", files: map[string]string{
			"model/model.go": model, "p_test.go": "package p\n\n// guarantee: model.Gone\nfunc TestGone() {}\n"},
			wantUnmatched: "Gate,Group", wantStale: "model.Gone"},
		{name: "a package that is not there is stale", files: map[string]string{
			"p_test.go": "package p\n\n// guarantee: nowhere.Gate\nfunc TestGate() {}\n"},
			wantStale: "nowhere.Gate"},
		{name: "a directory name two directories share is no address", files: map[string]string{
			"model/model.go": model, "internal/model/model.go": model, "p_test.go": "package p\n\n// guarantee: model.Gate\nfunc TestGate() {}\n"},
			wantUnmatched: "Gate,Gate,Group,Group", wantStale: "model.Gate"},
		{name: "a name of the own package that has a dot stays there", files: map[string]string{
			"model/model.go": model, "root.go": "package p\n\n// Close sends nothing.\nfunc (*Client) Close() {}\n",
			"p_test.go": "package p\n\n// guarantee: Client.Close\nfunc TestClose() {}\n"},
			wantMatched: "Client.Close", wantUnmatched: "Gate,Group"},
		{name: "a marker of the package itself does not travel to another with the same identifier", files: map[string]string{
			"model/model.go": model, "wire/wire.go": wire, "model/model_test.go": "package model\n\n// guarantee: Gate\nfunc TestGate() {}\n"},
			wantMatched: "Gate", wantUnmatched: "Gate,Group"},
		{name: "the package may not name itself", files: map[string]string{
			"model/model.go": model, "model/model_test.go": "package model\n\n// guarantee: model.Gate\nfunc TestGate() {}\n"},
			wantUnmatched: "Gate,Group", wantStale: "model.Gate"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Scan(writeTree(t, tc.files))
			if err != nil {
				t.Fatal(err)
			}
			var stale []string
			for _, m := range res.Stale {
				stale = append(stale, m.Name)
			}
			if got := sortedNames(res.Matched); got != tc.wantMatched {
				t.Errorf("matched %q, want %q", got, tc.wantMatched)
			}
			if got := sortedNames(res.Unmatched); got != tc.wantUnmatched {
				t.Errorf("unmatched %q, want %q", got, tc.wantUnmatched)
			}
			if got := strings.Join(stale, ","); got != tc.wantStale {
				t.Errorf("stale %q, want %q", got, tc.wantStale)
			}
		})
	}
}

// sortedNames lists the names of findings sorted, so that a case does not depend on the order the
// directories are walked in.
func sortedNames(fs []Finding) string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Name)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}
