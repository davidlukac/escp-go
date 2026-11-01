package udp

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestListenBindsASharedPort(t *testing.T) {
	first, err := Listen(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	ap, err := AddrPort(first.LocalAddr())
	if err != nil || ap.Port() == 0 {
		t.Fatalf("AddrPort of the bound socket: %v, %v", ap, err)
	}
	second, err := Listen(context.Background(), int(ap.Port()))
	if err != nil {
		t.Skipf("the system does not share a UDP port (%v)", err) // the same skip as the sharing tests
	}
	second.Close()
}

func TestListenReportsAPortItCannotBind(t *testing.T) {
	if _, err := Listen(context.Background(), -1); err == nil {
		t.Fatal("a negative port was bound")
	}
}

// stubConn is a packet connection that is not a *net.UDPConn.
type stubConn struct {
	net.PacketConn
	closed bool
}

func (s *stubConn) Close() error { s.closed = true; return nil }

func TestAsConnRefusesAnotherKindOfConnectionAndClosesIt(t *testing.T) {
	stub := &stubConn{}
	if c, err := asConn(stub); c != nil || err == nil || !strings.Contains(err.Error(), "not a UDP connection") || !stub.closed {
		t.Fatalf("got %v, %v (closed %t)", c, err, stub.closed)
	}
}

func TestAddrPortRefusesAnotherKindOfAddress(t *testing.T) {
	if _, err := AddrPort(&net.TCPAddr{}); err == nil || !strings.Contains(err.Error(), "not a UDP address") {
		t.Fatalf("got %v", err)
	}
	ap, err := AddrPort(&net.UDPAddr{IP: net.IPv4(192, 0, 2, 1), Port: 29808})
	if err != nil || ap.String() != "192.0.2.1:29808" {
		t.Fatalf("got %v, %v", ap, err)
	}
}
