//go:build live

package escp

import (
	"context"
	"net"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/internal/wire"
)

// replyRecorder wraps the client's socket and notes the header of every datagram it receives.
type replyRecorder struct {
	datagramConn
	c    *Client
	seen map[string]int // "op/errcode kind" -> datagrams
	zero map[string]int // the same, for those whose switch MAC is all zero
}

func (r *replyRecorder) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	n, from, err := r.datagramConn.ReadFromUDP(b)
	if err != nil {
		return n, from, err
	}
	if h, _, uerr := wire.Unmarshal(r.c.decrypt(append([]byte(nil), b[:n]...))); uerr == nil {
		key := "op " + itoa(int(h.Op))
		if h.ErrCode != 0 {
			key += " error reply"
		}
		if h.Fragment != 0 {
			key += " fragment"
		}
		r.seen[key]++
		if MAC(h.SwitchMAC).IsZero() {
			r.zero[key]++
		}
	}
	return n, from, err
}

func itoa(n int) string { return PortID(n).String() }

// TestLiveEveryReplyCarriesTheSwitchMAC records the switch MAC in the header of every kind of reply
// a client receives: the key exchange, every read, an error reply, a download in fragments and the
// acknowledgment of a same-value write (the LED, only with ESCP_ALLOW_WRITE=1). Reads otherwise.
// The evidence for refusing a reply without a MAC (PROTOCOL.md).
func TestLiveEveryReplyCarriesTheSwitchMAC(t *testing.T) {
	c, ctx := liveClient(t), context.Background()
	if err := c.open(ctx); err != nil {
		t.Fatal(err)
	}
	rec := &replyRecorder{datagramConn: c.conn, c: c, seen: map[string]int{}, zero: map[string]int{}}
	c.conn = rec
	c.session = nil // so that the key exchange is recorded too
	reads := []func() error{
		func() error { _, err := c.Info(ctx); return err },
		func() error { _, err := c.System(ctx); return err },
		func() error { _, err := c.Ports(ctx); return err },
		func() error { _, err := c.LAGs(ctx); return err },
		func() error { _, err := c.PortMirror(ctx); return err },
		func() error { _, err := c.Bandwidth(ctx); return err },
		func() error { _, err := c.StormControl(ctx); return err },
		func() error { _, err := c.QoS(ctx); return err },
		func() error { _, err := c.IGMP(ctx); return err },
		func() error { _, err := c.LoopPrevention(ctx); return err },
		func() error { _, err := c.VLANMode(ctx); return err },
		func() error { _, err := c.MTUVLAN(ctx); return err },
		func() error { _, err := c.PortBasedVLAN(ctx); return err },
		func() error { _, err := c.Dot1Q(ctx); return err },
		func() error { _, err := c.PortStats(ctx); return err },
		func() error { _, err := c.ConfigBackup(ctx); return err }, // fragments
	}
	for i, read := range reads {
		if err := read(); err != nil {
			t.Errorf("read %d: %v", i, err)
		}
	}
	// An error reply: the management VLAN is not readable on every firmware (reply code -2).
	if _, err := c.getTLVs(ctx, wire.TLVType(11)); err != nil {
		t.Logf("TLV 11 (error reply expected on some firmware): %v", err)
	}
	if os.Getenv("ESCP_ALLOW_WRITE") == "1" {
		led, err := c.LED(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := c.SetLED(ctx, led); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(10 * time.Millisecond)

	kinds := make([]string, 0, len(rec.seen))
	for k := range rec.seen {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		t.Logf("%-28s %3d datagrams, %d without a switch MAC", k, rec.seen[k], rec.zero[k])
		if rec.zero[k] != 0 {
			t.Errorf("%s: %d replies carry an all-zero switch MAC", k, rec.zero[k])
		}
	}
}
