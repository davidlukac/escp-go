package escp

import (
	"context"
	"testing"

	"github.com/davidlukac/escp-go/escptest"
)

func TestLEDRoundTrip(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	ctx := context.Background()
	on, err := c.LED(ctx)
	if err != nil || !on {
		t.Fatalf("LED = %v, %v; want true", on, err)
	}
	if err := c.SetLED(ctx, false); err != nil {
		t.Fatal(err)
	}
	if on, _ := c.LED(ctx); on {
		t.Fatal("LED should be off")
	}
	if err := c.SetLED(ctx, true); err != nil {
		t.Fatal(err)
	}
	if led, writes := f.LED(), f.WriteCount(); !led || writes != 2 {
		t.Fatalf("fake state led=%v writes=%d", led, writes)
	}
}
