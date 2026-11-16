package escp

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
)

// The survivors of the extended mutation run (make mutation-extended, #195) in the files that are
// neither the model validators nor the identity and relocation code: the byte count of the log
// lines, the parsers that skip a TLV they do not own, the VLAN and PVID numbers read from two
// bytes, and the settle of a move on a client whose Close has begun. Each test names the mutant
// it kills.

// The "bytes" attribute of a request or reply line is the sum of the value bytes of its TLVs. A
// single TLV cannot tell "+=" from "=" (n starts at 0), and a sum of zero cannot tell "-=" from
// "+=", so the cases have two TLVs with values of different lengths (logging.go, tlvBytes).
func TestTLVBytesIsTheSumOfEveryValue(t *testing.T) {
	tests := []struct {
		name string
		tlvs []wire.TLV
		want int
	}{
		{"no TLV", nil, 0},
		{"one TLV with no value", []wire.TLV{{Type: wire.TLVLED}}, 0},
		{"one TLV of five bytes", []wire.TLV{{Type: wire.TLVLED, Value: make([]byte, 5)}}, 5},
		{"two TLVs, the longer one last", []wire.TLV{{Type: wire.TLVLED, Value: make([]byte, 3)}, {Type: wire.TLVPorts, Value: make([]byte, 4)}}, 7},
		{"two TLVs, the longer one first", []wire.TLV{{Type: wire.TLVLED, Value: make([]byte, 4)}, {Type: wire.TLVPorts, Value: make([]byte, 3)}}, 7},
		{"three TLVs, the middle one empty", []wire.TLV{{Type: wire.TLVLED, Value: make([]byte, 1)}, {Type: wire.TLVPorts}, {Type: wire.TLVPortStats, Value: make([]byte, 19)}}, 20},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tlvBytes(tc.tlvs); got != tc.want {
				t.Fatalf("tlvBytes = %d, want %d", got, tc.want)
			}
		})
	}
}

// A reply that names a TLV of another kind among the ones a parser owns is read to the end: the
// entries after the foreign TLV are kept (stats.go and storm.go, "continue" in the loop over the
// TLVs; as "break" the parser would return nothing for a foreign TLV at the front, and only the
// entries before it for one in the middle).
func TestParsersReadOnPastAForeignTLV(t *testing.T) {
	foreign := wire.TLV{Type: 9999, Value: []byte{1, 2, 3}}
	t.Run("port statistics", func(t *testing.T) {
		got, err := parsePortStats([]wire.TLV{
			foreign,
			tlvHex(t, wire.TLVPortStats, "0101061434f1b7000000002873fb8a00000000"),
			foreign,
			tlvHex(t, wire.TLVPortStats, "03010000000000000000000000000000000000"),
			foreign,
		})
		if err != nil {
			t.Fatal(err)
		}
		want := []PortStats{
			{ID: 1, Enabled: true, Link: "1000Mbps-full", TxGood: 0x1434f1b7, RxGood: 0x2873fb8a},
			{ID: 3, Enabled: true, Link: "link-down"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v\nwant %+v", got, want)
		}
	})
	t.Run("storm control", func(t *testing.T) {
		got, err := parseStorm([]wire.TLV{
			foreign,
			tlvHex(t, wire.TLVStormControl, "0200010100"+"00001388"),
			foreign,
			tlvHex(t, wire.TLVStormControl, "0300010100"+"00000000"),
			foreign,
		})
		if err != nil {
			t.Fatal(err)
		}
		want := []PortStorm{
			{Port: 2, UnknownUnicast: true, Multicast: true, Rate: 0x1388, Enabled: true},
			{Port: 3, UnknownUnicast: true, Multicast: true},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v\nwant %+v", got, want)
		}
	})
}

// A VLAN id and a PVID are two bytes, big end first. The captured vectors of the other tests have
// a zero high byte (VLAN 1, VLAN 10), which a shift the wrong way round reads just the same; the ids
// here have both bytes set (vlan_8021q.go, "<<" in the VLAN entry and in the PVID entry).
func TestParseDot1QReadsBothBytesOfAnID(t *testing.T) {
	state := tlvHex(t, wire.TLVVLAN8021Q, "01")
	tests := []struct {
		name    string
		id      string
		wantVID VLANID
	}{
		{"0x0102", "0102", 258},
		{"0x0a05", "0a05", 2565},
		{"the highest id, 4094", "0ffe", 4094},
		{"a high byte and a zero low byte", "0100", 256},
	}
	for _, tc := range tests {
		t.Run("VLAN "+tc.name, func(t *testing.T) {
			got, err := parseDot1Q([]wire.TLV{state, tlvHex(t, wire.TLVVLAN8021QVLAN, tc.id+"00000003"+"00000000")})
			if err != nil {
				t.Fatal(err)
			}
			if len(got.VLANs) != 1 || got.VLANs[0].ID != tc.wantVID {
				t.Fatalf("VLANs = %+v, want one with ID %d", got.VLANs, tc.wantVID)
			}
		})
		t.Run("PVID "+tc.name, func(t *testing.T) {
			got, err := parseDot1Q([]wire.TLV{state, tlvHex(t, wire.TLVVLAN8021QPVID, "02"+tc.id)})
			if err != nil {
				t.Fatal(err)
			}
			if want := []PortPVID{{Port: 2, PVID: tc.wantVID}}; !reflect.DeepEqual(got.PVIDs, want) {
				t.Fatalf("PVIDs = %+v, want %+v", got.PVIDs, want)
			}
		})
	}
}

// finishMove settles nothing on a client whose Close has begun: it returns the cut-short error at
// once, without asking the switch what it has (ip.go, "||" of the condition on ErrClosed and
// closing). In the window between the write returning another error and finishMove taking the
// lock, Close can begin; as "&&" the call would go on to the probe, which is logged as a warning
// and, on a client that is still open, sent. The second case hands finishMove an ErrClosed from a
// client that is open, which Close alone does not produce: the condition is "either", and each
// operand is pinned by its own case.
func TestFinishMoveDoesNotSettleAWriteCutShortByClose(t *testing.T) {
	const probeLine = "the write got no reply; asking the switch what it has"
	tests := []struct {
		name       string
		wrote      error
		closeFirst bool
	}{
		{"the write got no reply and Close has begun", ErrNoReply, true},
		{"the write was ended by Close and reports ErrClosed", fmt.Errorf("sending: %w", ErrClosed), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := escptest.Start(t)
			log, rec := newLogRecorder()
			c := dialLogged(t, f, "secret", log)
			t.Cleanup(func() { c.Close() })
			if tc.closeFirst {
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
			}
			sentBefore := f.Received()

			err := c.finishMove(context.Background(), moveOutcome{wrote: tc.wrote, sent: true, mac: escptest.DefaultMAC(), before: lostReplyBefore, want: lostReplyWanted})

			if !errors.Is(err, ErrClosed) || !errors.Is(err, ErrMoveUnconfirmed) {
				t.Fatalf("finishMove = %v, want the cut-short error (ErrClosed and ErrMoveUnconfirmed)", err)
			}
			if lines := rec.named(probeLine); len(lines) != 0 {
				t.Errorf("finishMove began to settle the write: %+v", lines)
			}
			if got := f.Received(); got != sentBefore {
				t.Errorf("finishMove sent %d datagram(s) while settling a write that Close cut short", got-sentBefore)
			}
		})
	}
}
