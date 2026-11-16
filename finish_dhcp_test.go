package escp

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
)

// finishDHCP settles nothing on a client whose Close has begun: it returns the cut-short error at
// once, without asking the switch for the lease (dhcp.go, "||" of the condition on ErrClosed and
// closing). In the window between the write returning another error and finishDHCP taking the lock,
// Close can begin; as "&&" the call would go on to look the switch up and wait for a lease that
// nobody will use. The second case hands finishDHCP an ErrClosed from a client that is open, which
// Close alone does not produce: the condition is "either", and each operand is pinned by its own case.
func TestFinishDHCPDoesNotWaitForALeaseOnAWriteCutShortByClose(t *testing.T) {
	tests := []struct {
		name       string
		wrote      error
		closeFirst bool
	}{
		{"the write got no reply and Close has begun", ErrNoReply, true},
		{"the write was ended by Close and reports ErrClosed", fmt.Errorf("sending: %w", ErrClosed), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := escptest.Start(t)
			// a short wait only bounds the run of a mutant that goes on to wait; the code returns at once
			c, lookups := dialLeasing(t, f, withMoveWait(100*time.Millisecond))
			if tc.closeFirst {
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
			}
			sentBefore := f.Received()

			err := c.finishDHCP(context.Background(), lostReplyBefore, tc.wrote)

			if !errors.Is(err, ErrClosed) || !errors.Is(err, ErrMoveUnconfirmed) {
				t.Fatalf("finishDHCP = %v, want the cut-short error (ErrClosed and ErrMoveUnconfirmed)", err)
			}
			if *lookups != 0 {
				t.Errorf("finishDHCP looked the switch up %d time(s) to wait for a lease after Close", *lookups)
			}
			if got := f.Received(); got != sentBefore {
				t.Errorf("finishDHCP sent %d datagram(s) while settling a write that Close cut short", got-sentBefore)
			}
		})
	}
}
