package model_test

import (
	"testing"

	escp "github.com/davidlukac/escp-go"
	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/safety"
)

// TestATestMayCrossTheLayers is clean on purpose: the layering rules apply to production files only, so
// a model test may use the root package, the safety package and the fake switch.
func TestATestMayCrossTheLayers(t *testing.T) {
	if escp.Name == "" || safety.Name == "" || escptest.Name == "" {
		t.Fatal("empty")
	}
}
