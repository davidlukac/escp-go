package escp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
)

// These tests make a hang an assertion failure. Each guards a place where a bug would not return a
// wrong value but block for ever: a lock that is kept, a retry loop that does not end, a search that
// does not stop. A blocked test fails nothing: it holds the run until a timeout, and mutation
// testing then counts the mutant as timed out and not as killed. Each test here therefore
// runs the call under a deadline and fails when the deadline passes.
//
// The file name sorts before the other test files on purpose: the package's tests run in file order
// and gremlins stops a mutant at its first failing test (-failfast), so a mutant that hangs a later test
// must already have failed one of these.

// hangGuard is how long a call that returns at once in a healthy run may take before it counts as hung.
const hangGuard = 3 * time.Second

// A call that returns, with a result or with an error, has let go of the client: the next call, from
// a client or from Close, is not left waiting. The check takes the lock itself, under a deadline.
func TestACallThatReturnsLeavesTheClientFree(t *testing.T) {
	cases := []struct {
		name string
		call func(ctx context.Context, c *Client) error
	}{
		{"ConfigBackup", func(ctx context.Context, c *Client) error { _, err := c.ConfigBackup(ctx); return err }},
		{"Info", func(ctx context.Context, c *Client) error { _, err := c.Info(ctx); return err }},
		{"LED", func(ctx context.Context, c *Client) error { _, err := c.LED(ctx); return err }},
		{"SetLED", func(ctx context.Context, c *Client) error { return c.SetLED(ctx, true) }},
		{"Ports", func(ctx context.Context, c *Client) error { _, err := c.Ports(ctx); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := escptest.Start(t)
			f.EnableModel(nil) // the readers of ports and the other settings
			c := dialFake(t, f, "secret")
			if err := tc.call(context.Background(), c); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), hangGuard)
			defer cancel()
			if err := c.lock(ctx); err != nil {
				c.mu.Unlock() // the hold that was kept: the Close of the cleanup would wait for it as well
				t.Fatalf("%s returned and left the client locked: %v", tc.name, err)
			}
			c.mu.Unlock()
		})
	}
}

// The configuration download repeats a round the switch answered with a file that cannot be used,
// and gives up after Retries+1 rounds with the reason.
func TestAConfigBackupOfUnusableFilesGivesUpAfterTheRetries(t *testing.T) {
	f := escptest.Start(t)
	f.SetBackupMode(escptest.BackupBadMagic) // every answer is a complete file that is not a configuration
	c := dialFake(t, f, "secret")
	ctx, cancel := context.WithTimeout(context.Background(), hangGuard)
	defer cancel()
	if _, err := c.ConfigBackup(ctx); !errors.Is(err, ErrInvalidConfigFile) {
		t.Fatalf("got %v, want ErrInvalidConfigFile after the last round (a deadline error means the rounds did not end)", err)
	}
}

// A reply whose last fragment never comes is asked for again Retries+1 times, then reported as no reply.
func TestAReplyWithoutItsLastFragmentIsAskedForAgainOnlyRetriesTimes(t *testing.T) {
	f := escptest.Start(t)
	f.SetBackupMode(escptest.BackupDropSecond)
	c := dialFake(t, f, "secret")
	c.cfg.Retries = 1
	c.cfg.Timeout = 100 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), hangGuard)
	defer cancel()
	if _, err := c.ConfigBackup(ctx); !errors.Is(err, ErrNoReply) {
		t.Fatalf("got %v, want ErrNoReply after the last attempt (a deadline error means the attempts did not end)", err)
	}
}

// A discovery with a stop condition returns when the condition holds, not when the window ends.
func TestADiscoveryEndsAsSoonAsItsConditionHolds(t *testing.T) {
	hostPort := escptest.FreeUDPPort(t)
	r := startResponder(t, hostPort, func(h wire.Header) [][]byte {
		return [][]byte{packet(wire.OpReply, h.Seq, discoverTLVs([6]byte{1, 2, 3, 4, 5, 6}, [4]byte{192, 0, 2, 7}, "sw"))}
	})
	ctx, cancel := context.WithTimeout(context.Background(), hangGuard)
	defer cancel()
	got, err := discoverUntil(ctx, DiscoverOptions{Window: time.Minute},
		loopbackEnv(t, r, hostPort, netInterface{Name: "en0", MAC: macOne, Broadcasts: lo}),
		func(found map[string]Info) bool { return len(found) == 1 })
	if err != nil || len(got) != 1 {
		t.Fatalf("got %d switches and %v, want one and no error (a deadline error means the search kept waiting for the window)", len(got), err)
	}
}

// Every datagram waiting in the socket is dropped before a request goes out, not only the first:
// the second one carries the number the request uses.
func TestEveryStaleDatagramInTheSocketIsDroppedAndNotOnlyTheFirst(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	ctx := context.Background()
	want, err := c.LED(ctx)
	if err != nil {
		t.Fatal(err)
	}
	leaveInTheSocket(t, c, discoveryShapedReply(c, discoverySeqBase)) // a number no request uses
	leaveInTheSocket(t, c, discoveryShapedReply(c, nextSeq(c.seq)))   // the number of the next request
	got, err := c.LED(ctx)
	if err != nil {
		t.Fatalf("the second stale reply was taken for the answer: %v", err)
	}
	if got != want {
		t.Fatalf("LED = %t, want %t", got, want)
	}
}
