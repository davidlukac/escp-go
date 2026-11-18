package main

import (
	"fmt"
	"strings"
	"testing"
)

// A negative or a huge coefficient (the budget overflows) makes every mutant time out at once, and a
// timeout counts as killed: the gate would say ok on no evidence. The command takes only a plain
// integer from 1 to 9999: `010` is octal 8 to gremlins and 10 to a decimal parser.
func TestTheCoefficientCommandAcceptsOnlyAPositiveInteger(t *testing.T) {
	tests := []struct {
		value string
		want  int
	}{
		{"1", exitOK}, {"3", exitOK}, {"10", exitOK}, {"250", exitOK}, {"9999", exitOK},
		{"0", exitUsage}, {"-1", exitUsage}, {"abc", exitUsage}, {"", exitUsage}, {"1.5", exitUsage}, {"10s", exitUsage}, {" 10", exitUsage},
		{"010", exitUsage}, {"+5", exitUsage}, {"10000", exitUsage}, {"300000000", exitUsage}, {"1000000000", exitUsage}, {"9223372036854775807", exitUsage},
		{"99999999999999999999", exitUsage}, {"0x10", exitUsage}, {"1_0", exitUsage}, {"1e1", exitUsage}, {"00", exitUsage}, {"-0", exitUsage}, {"10\n", exitUsage},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			code, out, errOut := runWith(t, "coefficient", tt.value)
			if code != tt.want || out != "" || (tt.want != exitOK) != strings.Contains(errOut, fmt.Sprintf("the timeout coefficient %q is not a plain integer from 1 to 9999", tt.value)) {
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
