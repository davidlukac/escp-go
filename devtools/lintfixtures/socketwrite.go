package escp

import (
	"net"
	"net/netip"
)

// WriteFromAnywhere writes to a socket from a file that is not the transport: one finding for each of the
// five write methods the rule names, WriteTo through the net.PacketConn interface the way a client that
// holds an interface writes.
func WriteFromAnywhere(packet net.PacketConn, conn *net.UDPConn, to net.Addr, peer *net.UDPAddr, port netip.AddrPort) {
	_, _ = packet.WriteTo(nil, to)
	_, _ = conn.WriteToUDP(nil, peer)
	_, _ = conn.WriteToUDPAddrPort(nil, port)
	_, _, _ = conn.WriteMsgUDP(nil, nil, peer)
	_, _, _ = conn.WriteMsgUDPAddrPort(nil, nil, port)
}
