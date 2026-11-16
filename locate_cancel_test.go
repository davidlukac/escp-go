package escp

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
)

// The context of Locate and Discover (DC-L01). Both are one search, discoverUntil, which binds the
// reply port, sends the probes and waits for replies in collectReplies; Locate adds the condition
// that ends the wait at the first reply of the wanted switch. So the same table of cancellations
// runs against both entry points, and a change to either wait is covered by it.
//
// Nothing here sleeps. The window is a minute and is never reached: each case ends by its context
// or, in the guard case, by the reply, and a call that does not end is cut off by the failure
// bound of the test. The one case that waits for a deadline to pass uses a short timeout whose
// length decides nothing but how soon the answer comes.

// cancelFailureBound is how long a test waits for a call that must have ended before it fails.
const cancelFailureBound = 10 * time.Second

// searchWindow is longer than any case waits: a call that returns only when it is over fails.
const searchWindow = time.Minute

// searchCall is one entry point of the search, run against the made-up network of a case.
type searchCall struct {
	name string
	run  func(ctx context.Context, env discoverEnv) error
}

var searchCalls = []searchCall{
	{"Locate", func(ctx context.Context, env discoverEnv) error {
		_, err := locateWith(ctx, macTwo, DiscoverOptions{Window: searchWindow}, env)
		return err
	}},
	{"Discover", func(ctx context.Context, env discoverEnv) error {
		_, err := discoverWith(ctx, DiscoverOptions{Window: searchWindow}, env)
		return err
	}},
}

// runBounded runs the call and returns its error, failing the test when it has not returned within
// the failure bound: the call is the thing that must not hang, so it cannot run on the test's goroutine.
func runBounded(t *testing.T, call func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- call() }()
	select {
	case err := <-done:
		return err
	case <-time.After(cancelFailureBound):
		t.Fatalf("the call did not end with its context within %v", cancelFailureBound)
		return nil
	}
}

func canceledContext(t *testing.T) (context.Context, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	return ctx, nil
}

func expiredContext(t *testing.T) (context.Context, func()) {
	t.Helper()
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Hour))
	t.Cleanup(cancel)
	return ctx, nil
}

// cancelingContext is a context that the switch (or the write hook) cancels: the function returned is the cancel.
func cancelingContext(t *testing.T) (context.Context, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	return ctx, cancel
}

func shortDeadlineContext(t *testing.T) (context.Context, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	t.Cleanup(cancel)
	return ctx, nil
}

// guarantee: Discover
// guarantee: Locate
func TestSearchEndsWhenItsContextEnds(t *testing.T) {
	other := func(h wire.Header) [][]byte { // a switch that is not the one wanted
		return [][]byte{packet(wire.OpReply, h.Seq, discoverTLVs(macOne, [4]byte{192, 0, 2, 9}, "other"))}
	}
	tests := []struct {
		name string
		// context builds the context of the call and, for a cancel that the switch makes, the
		// function that is called when the probe arrives at the responder, or, with cancelOnSend,
		// as soon as the call has written it (nil: nothing happens).
		context      func(t *testing.T) (ctx context.Context, onProbe func())
		cancelOnSend bool
		replies      func(h wire.Header) [][]byte
		want         error // errors.Is
		// probesSent: whether the call may have sent a probe. False asserts none was written.
		probesSent bool
	}{
		{name: "canceled before the call", context: canceledContext, want: context.Canceled},
		{name: "deadline passed before the call", context: expiredContext, want: context.DeadlineExceeded},
		{name: "canceled while waiting for a reply", context: cancelingContext, want: context.Canceled, probesSent: true},
		{
			// The context ends between the send and the first wait, where the wake-up that it sends to
			// the socket can land before the wait sets its deadline over it.
			name:    "canceled as soon as the probe is written",
			context: cancelingContext, cancelOnSend: true, want: context.Canceled, probesSent: true,
		},
		{
			name:    "canceled while other switches answer",
			context: cancelingContext, replies: other, want: context.Canceled, probesSent: true,
		},
		{
			// The switch never answers; the timeout only says how soon the wait ends.
			name:    "deadline passes while waiting for a reply",
			context: shortDeadlineContext, want: context.DeadlineExceeded, probesSent: true,
		},
	}
	for _, call := range searchCalls {
		for _, tc := range tests {
			t.Run(call.name+"/"+tc.name, func(t *testing.T) {
				hostPort := escptest.FreeUDPPort(t)
				ctx, onProbe := tc.context(t)
				r := startResponder(t, hostPort, func(h wire.Header) [][]byte {
					if onProbe != nil && !tc.cancelOnSend {
						onProbe()
					}
					if tc.replies == nil {
						return nil
					}
					return tc.replies(h)
				})
				var written atomic.Int32
				env := loopbackEnv(t, r, hostPort, netInterface{Name: "en0", MAC: macOne, Broadcasts: lo})
				env.write = func(conn *net.UDPConn, pkt []byte, to *net.UDPAddr) error {
					written.Add(1)
					_, err := conn.WriteToUDP(pkt, to)
					if onProbe != nil && tc.cancelOnSend {
						onProbe()
					}
					return err
				}
				err := runBounded(t, func() error { return call.run(ctx, env) })
				if !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}
				if errors.Is(err, ErrNotFound) {
					t.Fatalf("a call ended by its context reported %v: the caller could not tell it from a switch that is gone", err)
				}
				var netErr net.Error
				if errors.Is(tc.want, context.DeadlineExceeded) && (!errors.As(err, &netErr) || !netErr.Timeout()) {
					t.Fatalf("%v does not satisfy net.Error with Timeout() true", err)
				}
				if !tc.probesSent && written.Load() != 0 {
					t.Fatalf("%d probes were sent for a call whose context had ended before it began", written.Load())
				}
			})
		}
	}
}

// The guard of the table above: with a live context the same setup finds the switch, so the cases
// that fail are failing because of their context and not because the setup cannot find anything.
func TestLocateWithALiveContextFindsTheSwitchThatAnswers(t *testing.T) {
	hostPort := escptest.FreeUDPPort(t)
	r := startResponder(t, hostPort, func(h wire.Header) [][]byte {
		return [][]byte{
			packet(wire.OpReply, h.Seq, discoverTLVs(macOne, [4]byte{192, 0, 2, 9}, "other")),
			packet(wire.OpReply, h.Seq, discoverTLVs(macTwo, [4]byte{192, 0, 2, 10}, "wanted")),
		}
	})
	env := loopbackEnv(t, r, hostPort, netInterface{Name: "en0", MAC: macOne, Broadcasts: lo})
	var found Info
	err := runBounded(t, func() (err error) {
		found, err = locateWith(t.Context(), macTwo, DiscoverOptions{Window: searchWindow}, env)
		return err
	})
	if err != nil || found.MAC != macTwo || found.Hostname != "wanted" {
		t.Fatalf("got %+v, %v; want the wanted switch", found, err)
	}
}
