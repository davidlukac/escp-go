package escp

import (
	"context"
	"errors"
	"testing"

	"github.com/davidlukac/escp-go/escptest"
)

// a repeatable write that got no reply, followed by a failed re-key, is reported as a plain
// "session key exchange: no reply": the docs say that error means nothing was sent.
func TestAnUnconfirmedWriteSurvivesAFailedRekey(t *testing.T) {
	f := escptest.Start(t)
	c := dialFast(t, f, "secret")
	if err := c.SetLED(context.Background(), true); err != nil { // session established
		t.Fatal(err)
	}
	f.Configure(func(f *escptest.Faults) { f.DropSets = true; f.RSADrop = true })
	err := c.SetLED(context.Background(), false)
	var u *UnconfirmedWriteError
	t.Logf("err=%v ; As(*UnconfirmedWriteError)=%v ; ErrWriteUnconfirmed=%v ; datagrams of the write the switch dropped=%d", err, errors.As(err, &u), errors.Is(err, ErrWriteUnconfirmed), f.DroppedSets())
	if f.DroppedSets() == 0 {
		t.Fatal("the write never reached the switch")
	}
	if !errors.Is(err, ErrWriteUnconfirmed) {
		t.Errorf("a write was sent %d times and never answered, but the error says only %v", f.DroppedSets(), err)
	}
}
