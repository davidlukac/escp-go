package escptest

import (
	"net"
	"slices"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/internal/wire"
)

// dropPacketSent is the number of discovery probes the DropPacket test sends: the last one is never the
// dropped one, so its answer marks the end of what the switch will say.
const dropPacketSent = 5

// DropPacket(n) makes the switch ignore the nth datagram it receives, and only that one: the ones
// before it and after it are answered, and a number that no datagram reaches (zero, or past the last)
// drops none. The probes are sent in order from one socket, which loopback delivers in order, and the
// switch answers them in order, so the sequence numbers heard before the answer of the last probe are
// exactly the ones that were not dropped.
func TestDropPacketIgnoresTheNthDatagramOnly(t *testing.T) {
	tests := []struct {
		name string
		n    int
		want []uint16
	}{
		{"the first datagram", 1, []uint16{2, 3, 4, 5}},
		{"the second datagram", 2, []uint16{1, 3, 4, 5}},
		{"the third datagram", 3, []uint16{1, 2, 4, 5}},
		{"the fourth datagram", 4, []uint16{1, 2, 3, 5}},
		{"zero drops none", 0, []uint16{1, 2, 3, 4, 5}},
		{"a number past the last datagram drops none", dropPacketSent + 1, []uint16{1, 2, 3, 4, 5}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := Start(t)
			f.DropPacket(tc.n)
			peer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = peer.Close() })
			if err := peer.SetReadDeadline(time.Now().Add(receiveGuard)); err != nil {
				t.Fatal(err)
			}
			to := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: f.UDPPort()}
			for seq := uint16(1); seq <= dropPacketSent; seq++ {
				probe := wire.RC4Static(wire.Marshal(wire.Header{Op: wire.OpDiscovery, Seq: seq}, nil))
				if _, err := peer.WriteToUDP(probe, to); err != nil {
					t.Fatal(err)
				}
			}
			var heard []uint16
			buf := make([]byte, 4096)
			for len(heard) == 0 || heard[len(heard)-1] != dropPacketSent {
				n, _, err := peer.ReadFromUDP(buf)
				if err != nil {
					t.Fatalf("heard the answers to %v and then: %v (the answer to probe %d never came)", heard, err, dropPacketSent)
				}
				h, _, err := wire.Unmarshal(wire.RC4Static(buf[:n]))
				if err != nil {
					t.Fatal(err)
				}
				heard = append(heard, h.Seq)
			}
			if !slices.Equal(heard, tc.want) {
				t.Fatalf("DropPacket(%d): the switch answered probes %v, want %v", tc.n, heard, tc.want)
			}
			if got := f.Received(); got != dropPacketSent {
				t.Fatalf("the switch received %d datagrams, want %d (a dropped datagram is still received)", got, dropPacketSent)
			}
		})
	}
}
