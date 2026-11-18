package main

import (
	"strings"
	"testing"
)

// A negative coefficient makes every mutant time out at once, and a timeout counts as killed: the gate
// would say ok on no evidence. The command refuses what is not a positive integer.
func TestTheCoefficientCommandAcceptsOnlyAPositiveInteger(t *testing.T) {
	tests := []struct {
		value string
		want  int
	}{
		{"1", exitOK}, {"3", exitOK}, {"10", exitOK}, {"250", exitOK},
		{"0", exitUsage}, {"-1", exitUsage}, {"abc", exitUsage}, {"", exitUsage}, {"1.5", exitUsage}, {"10s", exitUsage}, {" 10", exitUsage},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			code, out, errOut := runWith(t, "coefficient", tt.value)
			if code != tt.want || out != "" || (tt.want != exitOK) != strings.Contains(errOut, "is not a positive integer") {
				t.Errorf("code %d (want %d), stdout %q, stderr %q", code, tt.want, out, errOut)
			}
		})
	}
}

func TestTheCoefficientCommandNeedsItsArgument(t *testing.T) {
	if code, _, errOut := runWith(t, "coefficient"); code != exitUsage || !strings.Contains(errOut, "usage") {
		t.Errorf("code %d, stderr %q", code, errOut)
	}
}
