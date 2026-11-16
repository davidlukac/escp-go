package wire

import "net"

// WriteFromAFileOfTheSameName writes to a socket from a file called transport.go that is not the root
// one: the path of the exclusion is anchored, so this stays a finding.
func WriteFromAFileOfTheSameName(conn *net.UDPConn, peer *net.UDPAddr) error {
	_, err := conn.WriteToUDP(nil, peer)
	return err
}
