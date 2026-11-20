package escp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/davidlukac/escp-go/escptest"
)

// A caller that cancels its own context with ErrClosed as the cause is told ErrClosed by closedBy
// (request.go) although Close was never called: closing() is false, and the write may have gone out.
// finishMove and finishDHCP treat an ErrClosed from the write as "Close ended it" on its own, so such
// a call is cut short (ErrClosed and ErrMoveUnconfirmed) and nothing is settled: no probe is sent and
// the client does not follow the switch. This pins that behavior of the `errors.Is(err, ErrClosed)`
// operand of both functions (an operand that is not an equivalent mutant: without it the probe runs
// and the call succeeds). The caller ends the call right after the switch applied the write, the
// same moment as in TestSetIPConfigSettlesWhenTheCallIsCanceledAfterTheWrite.
func TestACallerThatCancelsWithErrClosedAsTheCauseIsCutShortWithoutSettling(t *testing.T) {
	cutOff := func(t *testing.T, err error) {
		t.Helper()
		if !errors.Is(err, ErrClosed) || !errors.Is(err, ErrMoveUnconfirmed) {
			t.Fatalf("got %v, want the cut-short error (ErrClosed and ErrMoveUnconfirmed)", err)
		}
	}
	t.Run("SetIPConfig", func(t *testing.T) {
		f := escptest.Start(t)
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		var receivedAtWrite atomic.Int64
		f.Configure(func(faults *escptest.Faults) {
			faults.OnSystemSet = escptest.SystemSetLeaves
			faults.AfterSystemSet = func() {
				receivedAtWrite.Store(int64(f.Received()))
				cancel(ErrClosed)
			}
		})
		c := dialFake(t, f, "secret")
		before := c.dest.IP.String()
		cutOff(t, c.SetIPConfig(ctx, lostReplyWanted))
		if c.closing() {
			t.Fatal("the client was never closed: the test did not make the state it describes")
		}
		if got := int64(f.Received()); got != receivedAtWrite.Load() {
			t.Fatalf("the switch received %d datagram(s) after the write, want none (nothing is settled)", got-receivedAtWrite.Load())
		}
		if c.alsoFrom != nil || c.dest.IP.String() != before {
			t.Fatalf("the client must stay where it was: dest=%v alsoFrom=%v", c.dest.IP, c.alsoFrom)
		}
	})
	t.Run("EnableDHCP", func(t *testing.T) {
		f := escptest.Start(t)
		ctx, cancel := context.WithCancelCause(context.Background())
		defer cancel(nil)
		f.Configure(func(f *escptest.Faults) {
			f.OnSystemSet = escptest.SystemSetLeaves
			f.AfterSystemSet = func() { cancel(ErrClosed) }
		})
		c, lookups := dialLeasing(t, f)
		cutOff(t, c.EnableDHCP(ctx))
		if c.closing() {
			t.Fatal("the client was never closed: the test did not make the state it describes")
		}
		if *lookups != 0 {
			t.Fatalf("EnableDHCP looked the switch up %d time(s) for a lease after it was cut short, want none", *lookups)
		}
	})
}
