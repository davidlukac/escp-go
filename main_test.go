package escp

import (
	"os"
	"testing"
)

// claimsInAProgram is whether a client claims its switch by default in a program, before TestMain
// turns that off for the tests of this package.
var claimsInAProgram bool

// The tests put many clients on a few fake switches and loopback addresses, which the registry of
// switches in use (registry.go) would refuse; they claim only where they test the claim.
func TestMain(m *testing.M) {
	claimsInAProgram = claimsByDefault
	claimsByDefault = false
	os.Exit(m.Run())
}
