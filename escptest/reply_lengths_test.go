package escptest

import (
	"bytes"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/internal/invalid"
	"github.com/davidlukac/escp-go/internal/wire"
)

// An answer whose lengths do not fit the wire is not encoded, so reply does not send it: no switch
// could. The datagram of one that fits is a packet that parses and carries what was scripted.
// (A datagram this large is refused by the socket on common systems anyway, so the test looks at the
// encoder, which is where the rule is, and not at what arrives.)
func TestEncodeReplyRefusesAnAnswerThatDoesNotFitTheWire(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value int
		fits  bool
	}{
		{"a value of 70000 bytes", 70000, false},
		{"a value one over the TLV limit", wire.MaxTLVValue + 1, false},
		{"a value that overflows the packet by one byte", wire.MaxPacket - wire.HeaderLen - 2*wire.TLVHeaderLen + 1, false},
		{"the largest value a packet holds", wire.MaxPacket - wire.HeaderLen - 2*wire.TLVHeaderLen, true},
		{"a small value", 1000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := Start(t)
			tlvs := []wire.TLV{{Type: wire.TLVConfigFile, Value: bytes.Repeat([]byte{1}, tc.value)}}
			b, err := f.encodeReply(request{h: wire.Header{Seq: 3}}, wire.OpReply, 0, tlvs, false)
			if !tc.fits {
				if !errors.Is(err, invalid.Err) || b != nil {
					t.Fatalf("got %d bytes and %v, want no datagram and an invalid-argument error", len(b), err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			h, got, err := wire.Unmarshal(wire.RC4Static(b))
			if err != nil {
				t.Fatalf("the datagram does not parse: %v", err)
			}
			if h.Seq != 3 || len(got) != 1 || len(got[0].Value) != tc.value {
				t.Fatalf("seq %d and %d TLVs, the first of %d bytes; want seq 3 and one TLV of %d", h.Seq, len(got), len(got[0].Value), tc.value)
			}
		})
	}
}

// receiveGuard ends a read that would wait for a datagram that never comes; it is a failure guard,
// not something a passing run waits for.
const receiveGuard = 5 * time.Second

// reply sends nothing for an answer that does not fit: asked for the oversized answer and then for a
// small one, the peer hears the small one first (loopback keeps the order).
func TestReplyStaysSilentForAnAnswerThatDoesNotFitTheWire(t *testing.T) {
	f := Start(t)
	peer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	if err := peer.SetReadDeadline(time.Now().Add(receiveGuard)); err != nil {
		t.Fatal(err)
	}
	from, ok := peer.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("local address is a %T", peer.LocalAddr())
	}
	req := request{from: from, h: wire.Header{Seq: 3}}
	f.reply(req, wire.OpReply, 0, []wire.TLV{{Type: wire.TLVConfigFile, Value: make([]byte, 70000)}}, false)
	f.reply(req, wire.OpReply, 0, []wire.TLV{{Type: wire.TLVLED, Value: []byte{1}}}, false)
	buf := make([]byte, 4096)
	n, _, err := peer.ReadFromUDP(buf)
	if err != nil {
		t.Fatal(err)
	}
	_, tlvs, err := wire.Unmarshal(wire.RC4Static(buf[:n]))
	if err != nil || len(tlvs) != 1 || tlvs[0].Type != wire.TLVLED {
		t.Fatalf("the first datagram the peer got is not the small answer: %v, %v", tlvs, err)
	}
}
