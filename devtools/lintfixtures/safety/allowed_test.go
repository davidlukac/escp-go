package safety

import (
	"testing"

	"github.com/davidlukac/escp-go/escptest"
)

// TestATestMayUseTheFake is clean on purpose: the layering rules apply to production files only.
func TestATestMayUseTheFake(t *testing.T) {
	if escptest.Name == "" {
		t.Fatal("empty")
	}
}
