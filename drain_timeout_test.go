package escp

import (
	"context"
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

// drainConn is a socket for the drain tests that keeps its own time. It answers a read the way
// Go's poller does: a read whose deadline has already passed returns a timeout without looking at
// the socket, a read that finds a datagram returns it, and a read that finds none waits for the
// deadline. Nothing sleeps: the clock is the test's, and hurry says how much of it passes between
// a call to SetReadDeadline and the read that follows (a goroutine that was not scheduled).
type drainConn struct {
	datagramConn // the rest of the socket; the drain uses nothing else

	now      time.Time
	deadline time.Time
	queue    [][]byte
	hurry    func(call int) time.Duration
	failWith error  // returned by every read instead of the above
	failOnce error  // returned by the first read only
	onRead   func() // runs at the start of every read

	sets  int
	reads int
}

func (d *drainConn) clock() time.Time { return d.now }

func (d *drainConn) SetReadDeadline(t time.Time) error {
	d.deadline = t
	if d.hurry != nil {
		d.now = d.now.Add(d.hurry(d.sets))
	}
	d.sets++
	return nil
}

func (d *drainConn) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	d.reads++
	if d.onRead != nil {
		d.onRead()
	}
	if d.failOnce != nil {
		err := d.failOnce
		d.failOnce = nil
		return 0, nil, err
	}
	if d.failWith != nil {
		return 0, nil, d.failWith
	}
	timeout := &net.OpError{Op: "read", Net: "udp", Err: os.ErrDeadlineExceeded}
	if !d.now.Before(d.deadline) {
		return 0, nil, timeout // the poller checks the deadline before it tries the read
	}
	if len(d.queue) > 0 {
		n := copy(b, d.queue[0])
		d.queue = d.queue[1:]
		return n, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}, nil
	}
	d.now = d.deadline // nothing is waiting: the read blocks until the deadline
	return 0, nil, timeout
}

func queued(n int) [][]byte {
	q := make([][]byte, n)
	for i := range q {
		q[i] = []byte{byte(i)}
	}
	return q
}

func TestADrainTimeoutThatDidNotWaitDoesNotProveTheSocketEmpty(t *testing.T) {
	ever := func(int) time.Duration { return 2 * drainWait }
	everySecond := func(call int) time.Duration {
		if call%2 == 0 {
			return 2 * drainWait
		}
		return 0
	}
	once := func(call int) time.Duration {
		if call == 0 {
			return 3 * drainWait
		}
		return 0
	}
	waitOf := func(wait time.Duration) func(int) time.Duration { // the first read waits this long, the later ones the full drainWait
		return func(call int) time.Duration {
			if call == 0 {
				return drainWait - wait
			}
			return 0
		}
	}
	tests := []struct {
		name      string
		queue     int
		hurry     func(int) time.Duration
		failWith  error
		failOnce  error
		cancel    string // "before" or "during" the first read; empty for never
		wantReads int
		wantLeft  int
	}{
		{name: "an empty socket ends the drain at the first timeout", wantReads: 1},
		{name: "a timeout that came before the read tried does not hide the datagram", queue: 1, hurry: once, wantReads: 3},
		{name: "every other read hurried, all datagrams are dropped", queue: 3, hurry: everySecond, wantReads: 8},
		{name: "a flood ends at the bound", queue: 2 * maxDiscarded, wantReads: maxDiscarded, wantLeft: maxDiscarded},
		{name: "timeouts that never waited end at the bound too", queue: 1, hurry: ever, wantReads: maxDiscarded, wantLeft: 1},
		{name: "a wait of the minimum is a wait", hurry: waitOf(drainMinWait), wantReads: 1},
		{name: "a wait just below the minimum is not", hurry: waitOf(drainMinWait - 1), wantReads: 2},
		// the same boundary in literal values, so that changing drainMinWait fails a case
		{name: "a wait of 500 microseconds is a wait", hurry: waitOf(500 * time.Microsecond), wantReads: 1},
		{name: "a wait of 499 microseconds is not", hurry: waitOf(499 * time.Microsecond), wantReads: 2},
		{name: "a genuine empty wait ends the drain after one read", hurry: waitOf(drainWait - time.Microsecond), wantReads: 1},
		{name: "a closed socket ends the drain at once", queue: 1, failWith: net.ErrClosed, wantReads: 1, wantLeft: 1},
		{name: "any other read error ends it", queue: 1, failWith: syscall.EIO, wantReads: 1, wantLeft: 1},
		{name: "a read error that is only ICMP noise does not end it", queue: 2, failOnce: &net.OpError{Op: "read", Net: "udp", Err: errWSAConnReset}, wantReads: 4},
		{name: "ICMP noise that never stops ends at the bound", queue: 1, failWith: syscall.ECONNREFUSED, wantReads: maxDiscarded, wantLeft: 1},
		{name: "a context canceled before the drain reads nothing", queue: 1, cancel: "before", wantReads: 0, wantLeft: 1},
		{name: "a context canceled during a read ends it", queue: 1, hurry: ever, cancel: "during", wantReads: 1, wantLeft: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			conn := &drainConn{now: time.Unix(1000, 0), queue: queued(tt.queue), hurry: tt.hurry, failWith: tt.failWith, failOnce: tt.failOnce}
			switch tt.cancel {
			case "before":
				cancel()
			case "during":
				conn.onRead = cancel
			}
			c := &Client{conn: conn, seams: seams{clock: conn.clock}}
			c.discard(ctx)
			if conn.reads != tt.wantReads {
				t.Errorf("reads = %d, want %d", conn.reads, tt.wantReads)
			}
			if len(conn.queue) != tt.wantLeft {
				t.Errorf("datagrams left in the socket = %d, want %d", len(conn.queue), tt.wantLeft)
			}
		})
	}
}
