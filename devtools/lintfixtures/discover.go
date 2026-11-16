package escp

import (
	"fmt"
	"net"
	"net/netip"
	"time"
)

// Broadcast writes a datagram and sets a read deadline, as discover.go may: no finding.
func Broadcast(conn *net.UDPConn, peer netip.AddrPort) error {
	if _, err := conn.WriteToUDPAddrPort(nil, peer); err != nil {
		return err
	}
	if _, _, err := conn.WriteMsgUDP(nil, nil, &net.UDPAddr{}); err != nil {
		return err
	}
	if _, _, err := conn.WriteMsgUDPAddrPort(nil, nil, peer); err != nil {
		return err
	}
	return conn.SetReadDeadline(time.Time{})
}

// StillCheckedInDiscovery prints in discover.go: the exclusion of the file is for the socket rules only.
func StillCheckedInDiscovery() {
	fmt.Println("discovery")
}
