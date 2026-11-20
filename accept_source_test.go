package escp

import (
	"net"
	"testing"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
)

// acceptSourceSeq is the sequence number the client of TestAcceptTakesAReplyFromTheSwitchAndFromTheAddressItIsMovingTo
// is waiting for.
const acceptSourceSeq = 77

// accept (transport.go) takes a datagram as the reply to the request in flight only from the switch's
// address and, while the switch is being moved (alsoFrom is set, SetIPConfig), from the address it is
// moving to: the second address is one more, never a reason to take every reply, and "no second
// address" (nil) is not an address that a source could match. The last matters for a source with no
// address at all: net.IP(nil).Equal(net.IP(nil)) is true, so without the `c.alsoFrom == nil` operand a
// source that carries no address would match the absent second address and be taken as a reply. That
// operand is therefore required, not redundant: the case "no address, not moving" is the one that
// fails against the mutant that drops it.
func TestAcceptTakesAReplyFromTheSwitchAndFromTheAddressItIsMovingTo(t *testing.T) {
	var (
		current = net.IPv4(192, 0, 2, 1)
		moving  = net.IPv4(192, 0, 2, 2)
		third   = net.IPv4(192, 0, 2, 3)
	)
	tests := []struct {
		name     string
		alsoFrom net.IP
		from     net.IP
		seq      uint16
		want     bool
	}{
		{"the switch, not moving", nil, current, acceptSourceSeq, true},
		{"the switch, moving", moving, current, acceptSourceSeq, true},
		{"the switch written in its 4-byte form, not moving", nil, current.To4(), acceptSourceSeq, true},
		{"the new address, moving", moving, moving, acceptSourceSeq, true},
		{"the new address, not moving", nil, moving, acceptSourceSeq, false},
		{"a third address, moving", moving, third, acceptSourceSeq, false},
		{"a third address, not moving", nil, third, acceptSourceSeq, false},
		{"no address at all, not moving", nil, nil, acceptSourceSeq, false},
		{"no address at all, moving", moving, nil, acceptSourceSeq, false},
		{"the new address, moving, another request's sequence number", moving, moving, acceptSourceSeq + 1, false},
		{"the switch, not moving, another request's sequence number", nil, current, acceptSourceSeq + 1, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Dial(Config{Host: "192.0.2.1", SwitchPort: 1, HostPort: escptest.FreeUDPPort(t)})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = c.Close() })
			c.seq = acceptSourceSeq
			c.alsoFrom = tc.alsoFrom
			data := wire.RC4Static(wire.Marshal(wire.Header{Op: wire.OpReply, Seq: tc.seq}, nil))
			if _, ok := c.accept(&net.UDPAddr{IP: tc.from, Port: 1}, data); ok != tc.want {
				t.Fatalf("accept from %v with alsoFrom %v = %v, want %v", tc.from, tc.alsoFrom, ok, tc.want)
			}
		})
	}
}
