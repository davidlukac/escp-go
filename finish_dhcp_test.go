package escp

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
)

// finishDHCP settles nothing on a write that failed on a client whose Close has begun: it returns
// the cut-short error at once, without asking the switch for the lease (dhcp.go, "||" of the
// condition on ErrClosed and closing). In the window between the write returning an error and
// finishDHCP taking the lock, Close can begin; as "&&" the call would go on to look the switch up
// and wait for a lease that nobody will use.
func TestFinishDHCPDoesNotWaitForALeaseOnAFailedWriteCutShortByClose(t *testing.T) {
	f := escptest.Start(t)
	// a short wait only bounds the run of a mutant that goes on to wait; the code returns at once
	c, lookups := dialLeasing(t, f, withMoveWait(100*time.Millisecond))
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	sentBefore := f.Received()

	err := c.finishDHCP(context.Background(), lostReplyBefore, ErrNoReply)

	if !errors.Is(err, ErrClosed) || !errors.Is(err, ErrMoveUnconfirmed) {
		t.Fatalf("finishDHCP = %v, want the cut-short error (ErrClosed and ErrMoveUnconfirmed)", err)
	}
	if *lookups != 0 {
		t.Errorf("finishDHCP looked the switch up %d time(s) to wait for a lease after Close", *lookups)
	}
	if got := f.Received(); got != sentBefore {
		t.Errorf("finishDHCP sent %d datagram(s) while settling a write that Close cut short", got-sentBefore)
	}
}

// A write that was answered is settled even when Close began before the lock: the switch took the
// change, so the lease that is found at once is followed and the call returns nil (dhcp.go,
// "wrote != nil" of the same condition; without it the call would return the cut-short error).
func TestFinishDHCPFollowsTheLeaseOfAnAnsweredWriteThatCloseOvertook(t *testing.T) {
	f := escptest.Start(t)
	c, lookups := dialLeasing(t, f, withMoveWait(time.Second))
	leased := cfgOf("127.0.0.2", "255.255.0.0", "127.0.0.254")
	c.locator = func(context.Context, MAC) (Info, error) {
		*lookups++
		return Info{MAC: escptest.DefaultMAC(), IP: leased.IP, Netmask: leased.Netmask, Gateway: leased.Gateway, DHCP: true}, nil
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}

	err := c.finishDHCP(context.Background(), lostReplyBefore, nil)

	if err != nil {
		t.Fatalf("finishDHCP = %v, want nil: the write was answered and the lease is there", err)
	}
	if *lookups != 1 {
		t.Errorf("finishDHCP looked the switch up %d time(s), want 1", *lookups)
	}
	if want := net.IP(leased.IP.AsSlice()); !c.dest.IP.Equal(want) {
		t.Errorf("the client is at %v, want the leased address %v", c.dest.IP, want)
	}
}
