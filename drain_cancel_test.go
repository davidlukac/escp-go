package escp

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
)

// drainCutConn is the client's socket for a test that ends a call while its drain is reading: the
// first read after arm runs during (which cancels the call, closes the client or lets the call's
// deadline pass) and then times out, as a read that was interrupted does. Every datagram written
// is counted, so that "the call sent nothing" is what the socket saw and not what the code meant
// to do.
//
// A cancellation or a Close that happens while the read is running ends the call's context, and
// the context's end sets the read deadline (interruptOnDone): waitInterrupt makes the read wait for
// that, so that by the time the read returns the context is done, whichever goroutine got there
// first. The wait is on a channel the interrupt closes, not on a clock.
type drainCutConn struct {
	datagramConn // the client's socket: deadlines, local address, Close, and the reads before arm

	mu            sync.Mutex
	armed         bool
	reading       bool
	during        func()
	waitInterrupt bool
	interrupted   chan struct{}
	interruptOnce sync.Once

	writes atomic.Int32
}

func (s *drainCutConn) arm(during func(), waitInterrupt bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.armed, s.during, s.waitInterrupt = true, during, waitInterrupt
	s.interrupted = make(chan struct{})
}

func (s *drainCutConn) SetReadDeadline(t time.Time) error {
	s.mu.Lock()
	if s.reading { // set while the read runs: the call's context ended and interrupted it
		s.interruptOnce.Do(func() { close(s.interrupted) })
	}
	s.mu.Unlock()
	return s.datagramConn.SetReadDeadline(t)
}

func (s *drainCutConn) WriteToUDP(b []byte, addr *net.UDPAddr) (int, error) {
	s.writes.Add(1)
	return s.datagramConn.WriteToUDP(b, addr)
}

func (s *drainCutConn) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	s.mu.Lock()
	armed, during, wait, interrupted := s.armed, s.during, s.waitInterrupt, s.interrupted
	if armed && !s.reading {
		s.reading = true
	} else {
		armed = false
	}
	s.mu.Unlock()
	if !armed {
		return s.datagramConn.ReadFromUDP(b)
	}
	during()
	if wait {
		awaitUntil(func() bool {
			select {
			case <-interrupted:
				return true
			default:
				return false
			}
		})
	}
	return 0, nil, &net.OpError{Op: "read", Net: "udp", Err: os.ErrDeadlineExceeded}
}

// deadlineCtx is a context whose deadline passes while its Err is still nil, which is what the
// context's own timer does when the socket's deadline (the same instant) fires first: expired reads
// the deadline directly for that reason, and a check of Err alone would miss it.
type deadlineCtx struct {
	context.Context
	passed atomic.Bool
}

func (d *deadlineCtx) Deadline() (time.Time, bool) {
	if d.passed.Load() {
		return time.Now().Add(-time.Second), true
	}
	return time.Now().Add(time.Hour), true
}

// steppingClock is a clock of the test: every look at it is one drainWait later than the one
// before, so that a read of the drain that times out has waited for it and ends the drain, with
// nothing to sleep for.
func steppingClock() func() time.Time {
	var mu sync.Mutex
	now := time.Unix(1000, 0)
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(drainWait)
		return now
	}
}

func TestACallEndedDuringTheDrainSendsNothing(t *testing.T) {
	setLED := func(ctx context.Context, c *Client) error { return c.SetLED(ctx, true) }
	info := func(ctx context.Context, c *Client) error { _, err := c.Info(ctx); return err }
	tests := []struct {
		name string
		call func(context.Context, *Client) error
		end  string // how the call ends during the drain: cancel, close or deadline
		want error
	}{
		{name: "a write canceled during the drain", call: setLED, end: "cancel", want: context.Canceled},
		{name: "a write closed during the drain", call: setLED, end: "close", want: ErrClosed},
		{name: "a write whose deadline passes during the drain", call: setLED, end: "deadline", want: context.DeadlineExceeded},
		{name: "a read canceled during the drain", call: info, end: "cancel", want: context.Canceled},
		{name: "a read closed during the drain", call: info, end: "close", want: ErrClosed},
		{name: "a read whose deadline passes during the drain", call: info, end: "deadline", want: context.DeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := escptest.Start(t)
			f.EnableState()
			c := dialFake(t, f, "secret")
			if _, err := c.LED(context.Background()); err != nil { // the session is open: the call below is one exchange
				t.Fatal(err)
			}
			conn := &drainCutConn{datagramConn: c.conn}
			c.conn = conn
			c.clock = steppingClock()
			seq, received, led := c.seq, f.Received(), f.LED()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var closed sync.WaitGroup
			var during func()
			callCtx := ctx
			switch tt.end {
			case "cancel":
				during = cancel
			case "close":
				during = func() {
					closed.Go(func() { _ = c.Close() }) // Close waits for the call, which is in this read
				}
			case "deadline":
				dc := &deadlineCtx{Context: ctx}
				callCtx, during = dc, func() { dc.passed.Store(true) }
			}
			conn.arm(during, tt.end != "deadline") // a deadline that passes interrupts nothing: the read just times out
			err := tt.call(callCtx, c)
			closed.Wait()

			if !errors.Is(err, tt.want) {
				t.Errorf("error = %v, want %v", err, tt.want)
			}
			if closedErr := errors.Is(err, ErrClosed); closedErr != errors.Is(tt.want, ErrClosed) || errors.Is(err, ErrNoReply) {
				t.Errorf("error = %v, which says more than %v", err, tt.want)
			}
			if _, ok := errors.AsType[*UnconfirmedWriteError](err); ok {
				t.Errorf("error = %v: nothing was sent, so the outcome is not unconfirmed", err)
			}
			if got := conn.writes.Load(); got != 0 {
				t.Errorf("the socket saw %d writes, want 0", got)
			}
			if got := f.Received(); got != received {
				t.Errorf("the switch received %d datagrams, want none", got-received)
			}
			if f.LED() != led {
				t.Error("the switch applied the write")
			}
			if c.seq != seq {
				t.Errorf("sequence number = %d, want %d: the refused attempt used one", c.seq, seq)
			}
			if tt.end == "close" {
				if err := info(context.Background(), c); !errors.Is(err, ErrClosed) {
					t.Errorf("a call after Close = %v, want ErrClosed", err)
				}
				return
			}
			if err := tt.call(context.Background(), c); err != nil {
				t.Errorf("the next call on the same client failed: %v", err)
			}
		})
	}
}
