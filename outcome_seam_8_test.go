package escp

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
)

// the error of a SetIPConfig whose write was NOT applied now also matches
// ErrWriteUnconfirmed, whose text says it may have been applied.
func TestSetIPConfigNotAppliedDoesNotMatchWriteUnconfirmed(t *testing.T) {
	f := escptest.Start(t)
	f.Configure(func(f *escptest.Faults) { f.DropSets = true })
	c := dialFast(t, f, "secret")
	err := c.SetIPConfig(context.Background(), lostReplyWanted)
	var u *UnconfirmedWriteError
	t.Logf("err=%v\nErrWriteUnconfirmed=%v As(*UnconfirmedWriteError)=%v", err, errors.Is(err, ErrWriteUnconfirmed), errors.As(err, &u))
	if errors.Is(err, ErrWriteUnconfirmed) {
		t.Errorf("the call proved the write was not applied, yet it matches ErrWriteUnconfirmed (%q)", ErrWriteUnconfirmed)
	}
}

// A lease the client cannot follow (the address names more than one device) is reported with the
// address the switch has leased, so the caller knows where the switch is now.
func TestEnableDHCPNamesTheLeasedAddressWhenItCannotFollowIt(t *testing.T) {
	f := escptest.Start(t)
	c, _ := dialLeasing(t, f, withMoveWait(400*time.Millisecond), withLocator(func(context.Context, MAC) (Info, error) {
		return Info{MAC: escptest.DefaultMAC(), IP: netip.MustParseAddr("224.1.2.3"), Netmask: netip.MustParseAddr("255.255.255.0"), Gateway: netip.MustParseAddr("224.1.2.1"), DHCP: true}, nil
	}))
	err := c.EnableDHCP(context.Background())
	if err == nil || !strings.Contains(err.Error(), "the switch has leased 224.1.2.3") {
		t.Fatalf("got %v, want the leased address named", err)
	}
}
