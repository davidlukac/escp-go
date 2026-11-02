package escptest

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"syscall"
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
// client's bind with "address already in use". The reservation is a record in this process and a TCP
// listener on the same port number (the two port spaces are separate, so the number is free to
// mark), which the system releases when the process ends. On a system that forbids the TCP number
// (some Windows runners do, for ports in an excluded range) the port is handed out with the record
// alone: no other process of this kind can then see it. A program that binds the port without
// asking this package can still collide, and a client that binds late cannot rule that out.
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

// The ports this package has handed out are remembered for the life of the process (handed), and
// where the system allows it also marked with a TCP listener (markers) so that other processes see
// them. A port whose TCP number cannot be bound because somebody holds it is in use by another
// process of this kind (or by anything else that listens on TCP there), and is skipped. A port whose
// TCP number the system forbids for binding at all (Windows reports a port in an excluded range as
// "forbidden by its access permissions") cannot be marked; it is handed out unmarked, and the
// record of this process still keeps it from being handed out twice here.
var (
	markersMu sync.Mutex
	markers   []net.Listener
	handed    = map[int]bool{}
)

// listenMarker binds the TCP port that marks a UDP port as taken; tests replace it.
var listenMarker = func(port int) (net.Listener, error) {
	var lc net.ListenConfig
	return lc.Listen(context.Background(), "tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
}

// wsaeacces is the Windows socket error "an attempt was made to access a socket in a way forbidden
// by its access permissions". The standard library does not map it to os.ErrPermission, and the
// number is no errno on other systems, so the comparison is safe everywhere.
const wsaeacces = syscall.Errno(10013)

// markerForbidden reports whether err says the system forbids binding that TCP port, as opposed to
// somebody holding it.
func markerForbidden(err error) bool { return errors.Is(err, wsaeacces) }

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

// errHandedOut is why a port is refused that this process has handed out already.
var errHandedOut = errors.New("escptest: the port was handed out before")

// reserve takes the port: it listens on the TCP port of the same number, on the loopback address,
// until the process ends, and remembers the port. It fails when another listener holds the TCP port
// or when this process handed the port out before; where the system forbids the TCP port it takes
// the port without a marker.
func reserve(port int) error {
	markersMu.Lock()
	defer markersMu.Unlock()
	l, err := listenMarker(port)
	if err != nil && !markerForbidden(err) {
		return err
	}
	if handed[port] {
		if l != nil {
			_ = l.Close() // best effort: the port is refused, so its marker is not wanted
		}
		return errHandedOut
	}
	handed[port] = true
	if l != nil {
		markers = append(markers, l)
	}
	return nil
}
