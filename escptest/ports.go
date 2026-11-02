package escptest

import (
	"fmt"
	"net"
	"net/netip"
	"sync"
	"testing"

	"github.com/davidlukac/escp-go/internal/udp"
)

// Loopback is the address a fake switch listens on: a Config that aims at the fake gives it as
// BroadcastAddr, so discovery stays on this machine.
func Loopback() netip.Addr { return netip.MustParseAddr("127.0.0.1") }

// FreeUDPPort returns a currently unused UDP port so tests never bind the
// protocol's real client port 29809. The port is not handed out again by this package: the kernel
// may give a port it has just released to the very next bind (measured: about 1 in 10,000 when
// several goroutines bind at once), which would let a fake switch take the port a test was told to
// use and fail the client's bind with "address already in use". A port taken by something outside
// this package can still collide; a client that binds late cannot rule that out.
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

// reserved holds every port this package has handed out, for the life of the process.
var (
	reservedMu sync.Mutex
	reserved   = map[int]bool{}
)

// maxReservedTries bounds the search for a port nobody was given before.
const maxReservedTries = 100

// listenLoopback binds a UDP socket on a kernel-chosen loopback port.
func listenLoopback() (*net.UDPConn, error) {
	return net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
}

// listenUnreserved binds with listen until the kernel gives a port this package has not handed
// out before, and reserves it.
func listenUnreserved(listen func() (*net.UDPConn, error)) (*net.UDPConn, int, error) {
	var lastPort int
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
		reservedMu.Lock()
		taken := reserved[lastPort]
		reserved[lastPort] = true
		reservedMu.Unlock()
		if !taken {
			return c, lastPort, nil
		}
		_ = c.Close() // best effort: the port was already given to someone else, try another
	}
	return nil, 0, fmt.Errorf("escptest: no unreserved UDP port after %d tries (last %d)", maxReservedTries, lastPort)
}
