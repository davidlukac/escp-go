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
// The datagram is refused too when it comes from the right address with the right sequence number but
// is cut short (a TLV that claims more bytes than the packet holds): wire.Unmarshal then returns a
// filled-in header together with an error, and only the `err != nil` test of accept refuses it.
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
		// truncated makes the datagram's first TLV claim more bytes than the packet holds: wire.Unmarshal
		// then returns the header, filled in, together with an error.
		truncated bool
		want      bool
	}{
		{"the switch, not moving", nil, current, acceptSourceSeq, false, true},
		{"the switch, moving", moving, current, acceptSourceSeq, false, true},
		{"the switch written in its 4-byte form, not moving", nil, current.To4(), acceptSourceSeq, false, true},
		{"the new address, moving", moving, moving, acceptSourceSeq, false, true},
		{"the new address, not moving", nil, moving, acceptSourceSeq, false, false},
		{"a third address, moving", moving, third, acceptSourceSeq, false, false},
		{"a third address, not moving", nil, third, acceptSourceSeq, false, false},
		{"no address at all, not moving", nil, nil, acceptSourceSeq, false, false},
		{"no address at all, moving", moving, nil, acceptSourceSeq, false, false},
		{"the new address, moving, another request's sequence number", moving, moving, acceptSourceSeq + 1, false, false},
		{"the switch, not moving, another request's sequence number", nil, current, acceptSourceSeq + 1, false, false},
		{"the switch, not moving, the right sequence number and a truncated TLV", nil, current, acceptSourceSeq, true, false},
		{"the new address, moving, the right sequence number and a truncated TLV", moving, moving, acceptSourceSeq, true, false},
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
			b := wire.Marshal(wire.Header{Op: wire.OpReply, Seq: tc.seq}, nil)
			if tc.truncated {
				copy(b[wire.HeaderLen:], []byte{0x00, 0x05, 0x00, 0x64}) // a TLV of type 5 that says it holds 100 bytes
			}
			data := wire.RC4Static(b)
			if _, ok := c.accept(&net.UDPAddr{IP: tc.from, Port: 1}, data); ok != tc.want {
				t.Fatalf("accept from %v with alsoFrom %v = %v, want %v", tc.from, tc.alsoFrom, ok, tc.want)
			}
		})
	}
}
