package escp

import (
	"errors"
	"net"
	"os"
	"sync"
	"testing"
)

// staleConn is the client's socket with datagrams put in front of it: a read returns the ones the
// test delivered, in order, before it reads the real socket. A test that leaves stale datagrams in
// the socket (leaveInTheSocket) goes through it and not through a second socket, because a send
// from a second socket returns when the system has taken the datagram, not when it is in the
// receive queue of the client's socket (#206): the drain, which stops at the first read that finds
// nothing, could then miss it, and a test that depends on that is a flake. A datagram delivered here
// is in the queue when deliver returns, and the test says exactly what the drain was handed.
//
// It also records, for each datagram the client writes, how many delivered datagrams were still
// waiting at that moment, which is what "dropped before the request goes out" means.
type staleConn struct {
	datagramConn // the client's real socket: reads that find the queue empty, writes, deadlines, Close
	from         *net.UDPAddr

	mu          sync.Mutex
	queue       [][]byte
	leftAtWrite []int
}

// staleConnOf returns the client's staleConn, putting one in front of its socket the first time.
func staleConnOf(c *Client) *staleConn {
	if s, ok := c.conn.(*staleConn); ok {
		return s
	}
	s := &staleConn{datagramConn: c.conn, from: &net.UDPAddr{IP: c.dest.IP, Port: c.dest.Port}}
	c.conn = s
	return s
}

// deliver puts a datagram in the queue, after the ones that are there.
func (s *staleConn) deliver(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = append(s.queue, append([]byte(nil), data...))
}

// waiting is the number of delivered datagrams no read has taken yet.
func (s *staleConn) waiting() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

// waitingAtWrites is, for each datagram the client wrote, the number of delivered datagrams that
// were still waiting when it was written.
func (s *staleConn) waitingAtWrites() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.leftAtWrite...)
}

func (s *staleConn) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	s.mu.Lock()
	if len(s.queue) > 0 {
		data := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()
		return copy(b, data), s.from, nil
	}
	s.mu.Unlock()
	return s.datagramConn.ReadFromUDP(b)
}

func (s *staleConn) WriteToUDP(b []byte, addr *net.UDPAddr) (int, error) {
	s.mu.Lock()
	s.leftAtWrite = append(s.leftAtWrite, len(s.queue))
	s.mu.Unlock()
	return s.datagramConn.WriteToUDP(b, addr)
}

// emptyConn is a socket on which every read times out and every write succeeds.
type emptyConn struct{ datagramConn }

func (emptyConn) ReadFromUDP([]byte) (int, *net.UDPAddr, error) {
	return 0, nil, &net.OpError{Op: "read", Net: "udp", Err: os.ErrDeadlineExceeded}
}

func (emptyConn) WriteToUDP(b []byte, _ *net.UDPAddr) (int, error) { return len(b), nil }

func TestAStaleDatagramIsInTheSocketWhenItsHelperReturns(t *testing.T) {
	c := &Client{conn: emptyConn{}, dest: &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 7}}
	leaveInTheSocket(t, c, []byte{1})
	leaveInTheSocket(t, c, []byte{2, 2})
	leaveInTheSocket(t, c, []byte{3})
	s := staleConnOf(c)
	if s != c.conn || staleConnOf(c) != s {
		t.Fatal("a second datagram put a second wrapper in front of the socket")
	}
	if got := s.waiting(); got != 3 {
		t.Fatalf("%d datagrams waiting right after the helper returned, want 3", got)
	}
	buf := make([]byte, maxDatagram)
	for _, want := range [][]byte{{1}, {2, 2}, {3}} { // subtests: the reads are one sequence, each takes the next datagram
		n, from, err := c.conn.ReadFromUDP(buf)
		if err != nil || string(buf[:n]) != string(want) {
			t.Fatalf("read %v, %v; want %v, nil (in the order they were left)", buf[:n], err, want)
		}
		if !from.IP.Equal(c.dest.IP) || from.Port != c.dest.Port {
			t.Fatalf("a stale datagram came from %v, want the switch's address %v", from, c.dest)
		}
	}
	if _, _, err := c.conn.ReadFromUDP(buf); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("a read after the last datagram returned %v, want what the real socket returns (a timeout here)", err)
	}
	if got := s.waiting(); got != 0 {
		t.Fatalf("%d datagrams waiting after all were read, want 0 (each is delivered once)", got)
	}
}

func TestTheStaleConnCountsWhatWasWaitingAtEachWrite(t *testing.T) {
	c := &Client{conn: emptyConn{}, dest: &net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 7}}
	leaveInTheSocket(t, c, []byte{1})
	leaveInTheSocket(t, c, []byte{2})
	buf := make([]byte, maxDatagram)
	if _, err := c.conn.WriteToUDP(buf, c.dest); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.conn.ReadFromUDP(buf); err != nil {
		t.Fatal(err)
	}
	if _, err := c.conn.WriteToUDP(buf, c.dest); err != nil {
		t.Fatal(err)
	}
	got := staleConnOf(c).waitingAtWrites()
	if len(got) != 2 || got[0] != 2 || got[1] != 1 {
		t.Fatalf("waiting at the writes = %v, want [2 1]", got)
	}
}
