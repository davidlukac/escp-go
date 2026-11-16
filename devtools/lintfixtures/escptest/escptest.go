// Package escptest stands for the fake switch of the library in the lint fixtures.
package escptest

import (
	"net"
	"time"
)

// Name is what the other fixtures read.
const Name = "escptest"

// Answer writes a datagram, which the fake switch may (it answers with its own socket): no finding.
func Answer(conn *net.UDPConn, peer *net.UDPAddr) error {
	_, err := conn.WriteToUDP(nil, peer)
	return err
}

// WaitForARequest sets a read deadline. The exclusion of the fake switch is for the socket write only, so
// this one stays a finding.
func WaitForARequest(conn *net.UDPConn) error {
	return conn.SetReadDeadline(time.Time{})
}
