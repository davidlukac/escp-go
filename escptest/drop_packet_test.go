package escptest

import (
	"net"
	"slices"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/internal/wire"
)

// dropPacketSent is the number of discovery probes the first DropPacket test sends: the last one is
// never the dropped one, so its answer marks the end of what the switch will say.
const dropPacketSent = 5

// prober sends discovery probes to a fake from one socket and hears the answers. Loopback delivers
// the datagrams of one socket in order and the switch answers in order, so the sequence numbers heard
// before the answer of the last probe sent are exactly the probes that were not dropped.
type prober struct {
	t    *testing.T
	peer *net.UDPConn
	to   *net.UDPAddr
}

func newProber(t *testing.T, f *Switch) *prober {
	t.Helper()
	peer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	if err := peer.SetReadDeadline(time.Now().Add(receiveGuard)); err != nil {
		t.Fatal(err)
	}
	return &prober{t: t, peer: peer, to: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: f.UDPPort()}}
}

// probe sends the probes with the sequence numbers first to last and returns the numbers the switch
// answered, in the order it answered them.
func (p *prober) probe(first, last uint16) []uint16 {
	p.t.Helper()
	for seq := first; seq <= last; seq++ {
		b := wire.RC4Static(wire.Marshal(wire.Header{Op: wire.OpDiscovery, Seq: seq}, nil))
		if _, err := p.peer.WriteToUDP(b, p.to); err != nil {
			p.t.Fatal(err)
		}
	}
	var heard []uint16
	buf := make([]byte, 4096)
	for len(heard) == 0 || heard[len(heard)-1] != last {
		n, _, err := p.peer.ReadFromUDP(buf)
		if err != nil {
			p.t.Fatalf("heard the answers to %v and then: %v (the answer to probe %d never came)", heard, err, last)
		}
		h, _, err := wire.Unmarshal(wire.RC4Static(buf[:n]))
		if err != nil {
			p.t.Fatal(err)
		}
		heard = append(heard, h.Seq)
	}
	return heard
}

// DropPacket(n) makes the switch ignore the nth datagram it receives, and only that one: the ones
// before it and after it are answered, and a number that no datagram reaches (zero, or past the last)
// drops none.
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
			heard := newProber(t, f).probe(1, dropPacketSent)
			if !slices.Equal(heard, tc.want) {
				t.Fatalf("DropPacket(%d): the switch answered probes %v, want %v", tc.n, heard, tc.want)
			}
			if got := f.Received(); got != dropPacketSent {
				t.Fatalf("the switch received %d datagrams, want %d (a dropped datagram is still received)", got, dropPacketSent)
			}
		})
	}
}

// A later call replaces an earlier one (the last number wins), and a call made after datagrams have
// arrived counts from the first datagram the switch ever received, not from the call: neither the
// earlier number nor a restarted count may drop a datagram.
func TestDropPacketCanBeCalledAgain(t *testing.T) {
	t.Run("the last call replaces the earlier one", func(t *testing.T) {
		f := Start(t)
		f.DropPacket(2)
		f.DropPacket(4)
		heard := newProber(t, f).probe(1, 5)
		if want := []uint16{1, 2, 3, 5}; !slices.Equal(heard, want) {
			t.Fatalf("the switch answered probes %v, want %v (only the 4th datagram is dropped)", heard, want)
		}
	})
	t.Run("a call after datagrams arrived counts from the first one", func(t *testing.T) {
		f := Start(t)
		p := newProber(t, f)
		if heard := p.probe(1, 3); !slices.Equal(heard, []uint16{1, 2, 3}) {
			t.Fatalf("with nothing to drop the switch answered probes %v, want 1, 2 and 3", heard)
		}
		f.DropPacket(5) // the fifth datagram of the switch, the second one after this call
		heard := p.probe(4, 6)
		if want := []uint16{4, 6}; !slices.Equal(heard, want) {
			t.Fatalf("the switch answered probes %v, want %v (the 5th datagram it ever received is dropped)", heard, want)
		}
		if got := f.Received(); got != 6 {
			t.Fatalf("the switch received %d datagrams, want 6 (the count is not reset by DropPacket)", got)
		}
	})
}
