package escp

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"testing"
	"time"
)

// Waiting in a test is for an event, never for a guess of how long it takes (DC-C06, checked by the
// nosleep analyzer of devtools). A test that must know that a call has reached a place uses the seam
// that says so: a channel, a signal from the socket wrapper (signalingConn), the write gate
// (pairGate), the lookup it is blocked in, a line the library logs (logSignal), or, for a state that
// has no signal, awaitCondition. The two helpers below are the clock-free ones: they yield to the
// scheduler between looks and have no pause, and the bound they carry is the failure of the test, not
// the length of the wait.

// awaitBound is how long awaitCondition looks before the test fails. It is a limit for a test that
// would hang, generous because it is never waited out when the code is right.
const awaitBound = 20 * time.Second

// awaitUntil returns true once cond is true, and false after awaitBound. It looks again after
// yielding to the scheduler, so whatever has to run for cond to become true can run, with no pause
// between looks.
func awaitUntil(cond func() bool) bool {
	deadline := time.Now().Add(awaitBound)
	for !cond() {
		if time.Now().After(deadline) {
			return false
		}
		runtime.Gosched()
	}
	return true
}

// awaitCondition is awaitUntil that fails the test, naming what it waited for, when the bound passes.
func awaitCondition(t *testing.T, what string, cond func() bool) {
	t.Helper()
	if !awaitUntil(cond) {
		t.Fatalf("gave up after %v waiting for %s", awaitBound, what)
	}
}

// awaitQueued returns once a call is waiting for the lock of the client that another call holds (see
// queuedForModify: the wait of c.lock is the same goroutine as the wait of c.beginModify), so the test
// knows that the second call has reached the lock and cannot run before the first lets go.
func awaitQueued(t *testing.T) {
	t.Helper()
	awaitCondition(t, "a call to queue behind the one that holds the client", queuedForModify)
}

// logSignal is a slog handler that closes a channel when the library logs a message: the way a test
// knows that a call has reached the line of code that logs it. It keeps nothing else.
type logSignal struct {
	msg  string
	once *sync.Once
	seen chan struct{}
}

// newLogSignal returns a logger for Config.Logger or c.log and the channel that is closed when the
// library first logs msg, at any level.
func newLogSignal(msg string) (*slog.Logger, <-chan struct{}) {
	h := &logSignal{msg: msg, once: &sync.Once{}, seen: make(chan struct{})}
	return slog.New(h), h.seen
}

func (h *logSignal) Enabled(context.Context, slog.Level) bool { return true }
func (h *logSignal) Handle(_ context.Context, rec slog.Record) error {
	if rec.Message == h.msg {
		h.once.Do(func() { close(h.seen) })
	}
	return nil
}
func (h *logSignal) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *logSignal) WithGroup(string) slog.Handler      { return h }

// withLogger sets the logger of the client, as Config.Logger would (after the dial, so a test can
// give it to a client made by a helper).
func withLogger(l *slog.Logger) dialOption { return func(c *Client) { c.log = l } }
