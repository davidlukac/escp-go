package main

import (
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

// hostportFor returns the check pointed at the Config type of the fixtures (the default is the escp-go one).
func hostportFor(t *testing.T) *analysis.Analyzer {
	t.Helper()
	a := newHostport()
	if err := a.Flags.Set("type", `^hostporttests\.Config$`); err != nil {
		t.Fatal(err)
	}
	return a
}

// The cases are testdata/src/hostporttests/x_test.go: a Config literal that sets SwitchPort and not HostPort
// is reported (the shape of escp-go#237), and a literal with both ports, with the port assigned afterwards,
// handed to a function that owns the port, of another type, or annotated is not.
func TestHostportInTests(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), hostportFor(t), "hostporttests")
}

// Production code is not looked at: only a _test.go file binds a client for a test.
func TestHostportIgnoresProductionCode(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), hostportFor(t), "hostportprod")
}

// The type is a flag: with the default, which names the escp-go Config, the fixtures' own type is not one.
func TestHostportOnlyTheNamedType(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), newHostport(), "hostportprod")
}

// A pattern that does not compile is an error of the run.
func TestHostportBadPattern(t *testing.T) {
	if _, err := runHostport(nil, "("); err == nil {
		t.Fatal("a pattern that does not compile was accepted")
	}
}
