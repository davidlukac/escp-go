package escp

import (
	"os"
	"testing"
)

// The tests put many clients on a few fake switches and loopback addresses, which the registry of
// switches in use (registry.go) would refuse; they claim only where they test the claim.
func TestMain(m *testing.M) {
	claimsByDefault = false
	os.Exit(m.Run())
}
