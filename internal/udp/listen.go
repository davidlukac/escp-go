package udp

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"
)

// Listen binds a UDP socket on every local IPv4 address at the port, sharing the port with every
// other socket that shares it (Share).
func Listen(ctx context.Context, port int) (*net.UDPConn, error) {
	lc := net.ListenConfig{Control: Share}
	pc, err := lc.ListenPacket(ctx, "udp4", net.JoinHostPort("", strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	return asConn(pc)
}

// asConn is the *net.UDPConn behind a packet connection. A "udp4" listener always is one; the check
// keeps a different kind (which only a future change of the standard library could bring) an error
// instead of a panic.
func asConn(pc net.PacketConn) (*net.UDPConn, error) {
	c, ok := pc.(*net.UDPConn)
	if !ok {
		_ = pc.Close() // best effort: the connection is useless to the caller either way
		return nil, fmt.Errorf("udp: %T is not a UDP connection", pc)
	}
	return c, nil
}

// AddrPort is the address and port of a UDP endpoint, for an address that is not one an error.
func AddrPort(a net.Addr) (netip.AddrPort, error) {
	u, ok := a.(*net.UDPAddr)
	if !ok {
		return netip.AddrPort{}, fmt.Errorf("udp: %T is not a UDP address", a)
	}
	ap := u.AddrPort()
	return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port()), nil // net.IPv4 gives the 4-in-6 form
}
