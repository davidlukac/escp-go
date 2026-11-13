package escp

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
)

// setLEDOverhead is the size of the packet SetLED sends without the password: the header, the
// username TLV ("admin" and its NUL), the password TLV's header and its NUL, the LED TLV (one
// byte) and the end marker. Password length + setLEDOverhead is the size of the packet.
const setLEDOverhead = wire.HeaderLen + (wire.TLVHeaderLen + len("admin") + 1) + (wire.TLVHeaderLen + 1) + (wire.TLVHeaderLen + 1) + wire.TLVHeaderLen

// A request that does not fit the 16-bit wire lengths is refused before anything is sent: the only
// public way to make one is a Password (or Username) that is far too long, because everything else a
// request carries is validated short. The refusal leaves the client as it was: no datagram written, no
// sequence number used, and the next request works.
//
// guarantee: Client.SetLED
func TestARequestThatDoesNotFitTheWireLengthsIsRefusedBeforeAnythingIsSent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		password int    // the length of the password
		want     string // a part of the error: what is too large
	}{
		{"a password of 70000 bytes", 70000, "TLV type 514"},
		{"a value one over the TLV limit", wire.MaxTLVValue, "TLV type 514"},
		{"a value at the TLV limit, but the packet is over", wire.MaxTLVValue - 1, "packet"},
		{"a packet one over the limit", wire.MaxPacket - setLEDOverhead + 1, "packet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := escptest.Start(t)
			c := dialFake(t, f, Password(strings.Repeat("x", tc.password)))
			if _, err := c.LED(context.Background()); err != nil { // a read carries no credentials: it opens the session
				t.Fatal(err)
			}
			received, seq := f.Received(), c.seq

			err := c.SetLED(context.Background(), false)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("got %v, want an error that matches ErrInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "65535") {
				t.Fatalf("error %q does not name what is too large (%q) and the limit 65535", err, tc.want)
			}
			if strings.Contains(err.Error(), "xxxxxxxx") {
				t.Fatalf("error %q echoes the password", err)
			}
			if n := f.Received() - received; n != 0 {
				t.Fatalf("the refused request cost %d datagram(s) at the switch, want none", n)
			}
			if c.seq != seq {
				t.Fatalf("the refused request used a sequence number: %d, was %d", c.seq, seq)
			}
			if n := f.WriteCount(); n != 0 {
				t.Fatalf("the switch took %d write(s)", n)
			}

			// the client is as it was: the next requests work, and take the next sequence number
			c.cfg.Password = escptest.Password
			if err := c.SetLED(context.Background(), false); err != nil {
				t.Fatalf("a normal write after the refusal: %v", err)
			}
			if want := requestSeq(seq, wire.OpSet); c.seq != want {
				t.Fatalf("the write after the refusal used sequence number %d, want %d", c.seq, want)
			}
			if n := f.WriteCount(); n != 1 {
				t.Fatalf("the switch took %d write(s), want the one that fit", n)
			}
		})
	}
}

// writeRecorder records the datagrams the client writes and drops them: the largest packet is
// above what a loopback socket sends on some systems, and the test is about the packet, not the
// network.
type writeRecorder struct {
	datagramConn
	mu     sync.Mutex
	writes [][]byte
}

func (w *writeRecorder) WriteToUDP(b []byte, _ *net.UDPAddr) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writes = append(w.writes, append([]byte(nil), b...))
	return len(b), nil
}

// The limit itself is sent as before: the packet of the largest size that fits has a right length field and
// TLVs that parse, one byte more is refused. Sent through the unexported entry, with the socket
// replaced, because a datagram of 64 KiB does not go through every loopback.
func TestSendWritesThePacketAtTheLimitAndRefusesOneByteMore(t *testing.T) {
	const room = wire.MaxPacket - wire.HeaderLen - 2*wire.TLVHeaderLen // the longest value of a packet of one TLV
	for _, tc := range []struct {
		name  string
		value int
		sent  bool
	}{
		{"one byte below the limit", room - 1, true},
		{"at the limit", room, true},
		{"one byte over the limit", room + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := escptest.Start(t)
			c := dialFake(t, f, "secret")
			rec := &writeRecorder{datagramConn: c.conn}
			c.conn = rec
			seq := c.seq
			tlvs := []wire.TLV{{Type: wire.TLVConfigFile, Value: make([]byte, tc.value)}}

			err := c.send(context.Background(), wire.OpSet, tlvs, true, []*net.UDPAddr{c.dest})
			if !tc.sent {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("got %v, want ErrInvalid", err)
				}
				if len(rec.writes) != 0 || c.seq != seq {
					t.Fatalf("a refused request wrote %d datagram(s) and moved the sequence number from %d to %d", len(rec.writes), seq, c.seq)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(rec.writes) != 1 {
				t.Fatalf("%d datagrams written, want 1", len(rec.writes))
			}
			h, got, err := wire.Unmarshal(wire.RC4Static(rec.writes[0]))
			if err != nil {
				t.Fatalf("the packet does not parse: %v", err)
			}
			if int(h.Length) != len(rec.writes[0]) || len(got) != 1 || len(got[0].Value) != tc.value || h.Seq != requestSeq(seq, wire.OpSet) {
				t.Fatalf("length field %d for %d bytes, %d TLVs with a value of %d bytes, seq %d", h.Length, len(rec.writes[0]), len(got), len(got[0].Value), h.Seq)
			}
		})
	}
}

// A management write that is refused for its size has not reached the switch, so it is reported as
// refused, not settled as a write whose reply was lost (which would probe for the new address and
// report ErrMoveUnconfirmed, or wait for a lease).
//
// guarantee: Client.SetIPConfig
func TestAManagementWriteRefusedForItsSizeIsNotSettledAsALostReply(t *testing.T) {
	long := Password(strings.Repeat("x", wire.MaxPacket))
	for _, tc := range []struct {
		name string
		call func(c *Client) error
	}{
		{"SetIPConfig", func(c *Client) error { return c.SetIPConfig(context.Background(), lostReplyWanted) }},
		{"EnableDHCP", func(c *Client) error { return c.EnableDHCP(context.Background()) }},
		{"SetHostname", func(c *Client) error { return c.SetHostname(context.Background(), model.Hostname("sw-test")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := escptest.Start(t)
			f.EnableModel(nil)
			c, _ := dialLeasing(t, f) // pins the MAC, which EnableDHCP needs
			c.cfg.Password = long
			err := tc.call(c)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("got %v, want ErrInvalid", err)
			}
			if errors.Is(err, ErrMoveUnconfirmed) || errors.Is(err, ErrNoReply) {
				t.Fatalf("a request that was never sent is reported as a lost one: %v", err)
			}
			if n := f.WriteCount(); n != 0 {
				t.Fatalf("the switch took %d write(s)", n)
			}
		})
	}
}
