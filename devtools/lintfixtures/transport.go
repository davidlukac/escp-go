package escp

import (
	"net"
	"time"
)

// SendFromTheTransport writes a datagram and sets a read deadline, the two things only transport.go and
// discover.go may do: no finding.
func SendFromTheTransport(conn *net.UDPConn, peer *net.UDPAddr, packet net.PacketConn, to net.Addr) error {
	if _, err := conn.WriteToUDP(nil, peer); err != nil {
		return err
	}
	if _, err := packet.WriteTo(nil, to); err != nil {
		return err
	}
	return conn.SetReadDeadline(time.Time{})
}

// StillCheckedInTheTransport uses time.After in transport.go: the exclusion of the file is for the socket
// rules only, so this stays a finding.
func StillCheckedInTheTransport() {
	<-time.After(time.Second)
}
