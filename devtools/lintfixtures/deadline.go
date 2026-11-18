package escp

import (
	"net"
	"time"
)

// SetDeadlineFromAnywhere sets a read deadline outside the transport: one finding through the net.Conn
// interface and one through the concrete socket type.
func SetDeadlineFromAnywhere(conn net.Conn, udp *net.UDPConn) {
	_ = conn.SetReadDeadline(time.Time{})
	_ = udp.SetReadDeadline(time.Time{})
}
