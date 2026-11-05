package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/davidlukac/escp-go/model"
)

func TestRunPrintsTheParsedMACAndOneIdentifierOfEveryPackage(t *testing.T) {
	var out bytes.Buffer
	if err := run(&out, sampleMAC); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "00:00:5e:00:53:01 true admin 30s\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestRunRefusesAMACThatDoesNotParse(t *testing.T) {
	var out bytes.Buffer
	if err := run(&out, "not a mac"); !errors.Is(err, model.ErrInvalid) {
		t.Errorf("err = %v, want one that matches ErrInvalid", err)
	}
	if out.Len() != 0 {
		t.Errorf("printed %q before the error", out.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestRunReportsAWriteThatFails(t *testing.T) {
	if err := run(failingWriter{}, sampleMAC); err == nil {
		t.Error("a failed write was not reported")
	}
}
