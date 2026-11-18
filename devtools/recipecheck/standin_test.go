package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheStandInLeavesAMarkAndTheReport(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll("build", 0o750); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(dir, "good.json")
	if err := os.WriteFile(report, []byte(goodReportJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "log")
	env := map[string]string{"STANDIN_LOG": log, "STANDIN_REPORT": report}
	for range 2 {
		if code := runStandin(func(n string) string { return env[n] }, os.Stderr); code != exitOK {
			t.Fatalf("code %d", code)
		}
	}
	if data, err := os.ReadFile(log); err != nil || string(data) != "ran\nran\n" {
		t.Errorf("log %q (%v)", data, err)
	}
	if data, err := os.ReadFile(reportFile); err != nil || string(data) != goodReportJSON {
		t.Errorf("report %q (%v)", data, err)
	}
}

func TestTheStandInWithoutAReportLeavesNone(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	env := map[string]string{"STANDIN_LOG": filepath.Join(dir, "log")}
	if code := runStandin(func(n string) string { return env[n] }, os.Stderr); code != exitOK {
		t.Fatalf("code %d", code)
	}
	if _, err := os.Stat(reportFile); err == nil {
		t.Error("a report was written")
	}
	// and with neither variable it does nothing at all
	if code := runStandin(func(string) string { return "" }, os.Stderr); code != exitOK {
		t.Errorf("code %d", code)
	}
}

func TestTheStandInSaysWhatWentWrong(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	good := filepath.Join(dir, "good.json")
	if err := os.WriteFile(good, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"a log that cannot be opened", map[string]string{"STANDIN_LOG": filepath.Join(dir, "no", "such", "log")}},
		{"a report that cannot be read", map[string]string{"STANDIN_REPORT": filepath.Join(dir, "absent.json")}},
		{"a build directory that is missing", map[string]string{"STANDIN_REPORT": good}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if code := runStandin(func(n string) string { return tt.env[n] }, &stderr); code != exitError || !strings.Contains(stderr.String(), "recipecheck standin:") {
				t.Errorf("code %d, stderr %q", code, &stderr)
			}
		})
	}
}
