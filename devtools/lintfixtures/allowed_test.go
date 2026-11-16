package escp

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
)

// TestATestMayReachFurther is clean on purpose: a test may use the fake switch, a timer, a socket and
// the output, so no rule applies to a _test.go file.
func TestATestMayReachFurther(t *testing.T) {
	var conn net.PacketConn
	var tcp net.Conn
	if conn != nil {
		_, _ = conn.WriteTo(nil, nil)
	}
	if tcp != nil {
		_ = tcp.SetReadDeadline(time.Time{})
	}
	<-time.After(time.Nanosecond)
	fmt.Println(escptest.Name)
	println("done")
}
