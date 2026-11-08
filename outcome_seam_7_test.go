package escp

import (
	"context"
	"errors"
	"testing"

	"github.com/davidlukac/escp-go/escptest"
)

// a management write on a session the switch no longer knows is reported as unconfirmed
// although the switch cannot have applied it (it could not decrypt it); a cosmetic one re-keys.
func TestAManagementWriteOnAForgottenSessionProvesTheSessionFirst(t *testing.T) {
	f := escptest.Start(t)
	c := dialFast(t, f, "secret")
	if _, err := c.LED(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.ClearSession() // the switch forgot the key (rebooted, or another client re-keyed)
	err := c.SetLoopPrevention(context.Background(), true)
	var u *UnconfirmedWriteError
	t.Logf("management write: err=%v; unconfirmed=%v; writes applied=%d", err, errors.As(err, &u), f.WriteCount())
	if err != nil {
		t.Errorf("management write after the switch forgot the session: %v", err)
	}
	f.ClearSession()
	if err := c.SetLED(context.Background(), true); err != nil {
		t.Errorf("cosmetic write: %v", err)
	}
}
