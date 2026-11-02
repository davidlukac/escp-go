package escptest

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"testing"

	"github.com/davidlukac/escp-go/internal/udp"
)

// Loopback is the address a fake switch listens on: a Config that aims at the fake gives it as
// BroadcastAddr, so discovery stays on this machine.
func Loopback() netip.Addr { return netip.MustParseAddr("127.0.0.1") }

// FreeUDPPort returns a currently unused UDP port so tests never bind the
// protocol's real client port 29809. The port is reserved for the life of the process, and no
// other process that uses this package is given it either: the kernel hands a port it has just
// released to the very next bind, in this process (measured: about 1 in 10,000 when several
// goroutines bind at once) or in another one (the test binaries of one "go test ./..." run in
// parallel), and that would let a fake switch take the port a test was told to use and fail the
// client's bind with "address already in use". The reservation is a TCP listener on the same port
// number (the two port spaces are separate, so the number is free to mark), which the system
// releases when the process ends; a program that binds the port without asking this package can
// still collide, and a client that binds late cannot rule that out.
func FreeUDPPort(tb testing.TB) int {
	tb.Helper()
	port, err := FreePort()
	if err != nil {
		tb.Fatal(err)
	}
	return port
}

// FreePort is FreeUDPPort for code that is not a test, such as an example: it returns the error
// instead of failing a test.
func FreePort() (int, error) {
	c, port, err := listenUnreserved(listenLoopback)
	if err != nil {
		return 0, err
	}
	_ = c.Close() // best effort: only the port number is kept
	return port, nil
}

// markers holds the TCP listeners that reserve the ports this package has handed out, for the life
// of the process. A port whose TCP number cannot be bound is in use by another process of this kind
// (or by anything else that listens on TCP there), and is skipped.
var (
	markersMu sync.Mutex
	markers   []net.Listener
)

// maxReservedTries bounds the search for a port nobody was given before.
const maxReservedTries = 100

// listenLoopback binds a UDP socket on a kernel-chosen loopback port.
func listenLoopback() (*net.UDPConn, error) {
	return net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
}

// listenUnreserved binds with listen until the kernel gives a port that nobody holds a reservation
// for (this process or another), and reserves it.
func listenUnreserved(listen func() (*net.UDPConn, error)) (*net.UDPConn, int, error) {
	var lastPort int
	var lastErr error
	for range maxReservedTries {
		c, err := listen()
		if err != nil {
			return nil, 0, err
		}
		ap, err := udp.AddrPort(c.LocalAddr())
		if err != nil {
			_ = c.Close()
			return nil, 0, err
		}
		lastPort = int(ap.Port())
		if lastErr = reserve(lastPort); lastErr == nil {
			return c, lastPort, nil
		}
		_ = c.Close() // best effort: the port was already given to someone else, try another
	}
	return nil, 0, fmt.Errorf("escptest: no unreserved UDP port after %d tries (last %d: %w)", maxReservedTries, lastPort, lastErr)
}

// reserve marks the port as taken by listening on the TCP port of the same number, on the loopback
// address, until the process ends. It fails when another listener holds that TCP port.
func reserve(port int) error {
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	markersMu.Lock()
	markers = append(markers, l)
	markersMu.Unlock()
	return nil
}
