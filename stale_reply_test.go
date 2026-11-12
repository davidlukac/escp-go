package escp

import (
	"context"
	"testing"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
)

// discoveryShapedReply is what a switch answers to a discovery probe: an ordinary reply (the
// operation is the same as the answer to a read) carrying its identity TLVs, with the given
// sequence number. A hardware regression run read one of these as the answer to a LED read,
// a mirror read and a LAG read.
func discoveryShapedReply(c *Client, seq uint16) []byte {
	return wire.RC4Static(wire.Marshal(wire.Header{Op: wire.OpReply, SwitchMAC: c.ident.learned, Seq: seq},
		[]wire.TLV{{Type: 1, Value: []byte("TL-SG108E")}, {Type: 2, Value: []byte("switch")}, {Type: 3, Value: []byte{1, 2, 3, 4, 5, 6}}}))
}

// leaveInTheSocket leaves a datagram in the client's socket, from the switch's address, as a reply
// that was meant for another client of the same process (or for an earlier request) does. It is
// there when the function returns, by construction: see staleConn.
func leaveInTheSocket(t *testing.T, c *Client, data []byte) {
	t.Helper()
	staleConnOf(c).deliver(data)
}

func TestAStaleReplyInTheSocketIsNotTheAnswerToTheNextRequest(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	ctx := context.Background()
	want, err := c.LED(ctx) // opens the session; the switch's MAC is known now
	if err != nil {
		t.Fatal(err)
	}
	leaveInTheSocket(t, c, discoveryShapedReply(c, nextSeq(c.seq))) // carries the sequence number the next request will use
	got, err := c.LED(ctx)
	if err != nil {
		t.Fatalf("the stale reply was taken for the answer: %v", err)
	}
	if got != want {
		t.Fatalf("LED = %t, want %t", got, want)
	}
	if left := staleConnOf(c).waitingAtWrites(); len(left) == 0 || left[0] != 0 {
		t.Fatalf("datagrams still waiting when the request was written = %v, want none (the drain takes the stale reply before the request goes out)", left)
	}
}

func TestAClientDiscoveryDrawsItsSequenceNumberFromTheDiscoveryRange(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	ctx := context.Background()
	if _, err := c.Info(ctx); err != nil {
		t.Fatal(err)
	}
	if c.seq < discoverySeqBase || c.seq >= discoverySeqBase+discoverySeqSpace {
		t.Fatalf("discovery used sequence number %d, want one of %d-%d, which no request uses", c.seq, discoverySeqBase, discoverySeqBase+discoverySeqSpace-1)
	}
	if _, err := c.LED(ctx); err != nil {
		t.Fatal(err)
	}
	if c.seq >= clientSeqSpace {
		t.Fatalf("a request used sequence number %d, want one below %d", c.seq, clientSeqSpace)
	}
}

func TestAFloodOfStaleDatagramsDoesNotHoldARequestBack(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	ctx := context.Background()
	want, err := c.LED(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 * maxDiscarded { // none carries the number of the next request
		leaveInTheSocket(t, c, discoveryShapedReply(c, discoverySeqBase+uint16(i)))
	}
	got, err := c.LED(ctx)
	if err != nil || got != want {
		t.Fatalf("LED = %t, %v; want %t, nil", got, err, want)
	}
	if left := staleConnOf(c).waitingAtWrites(); len(left) == 0 || left[0] != maxDiscarded {
		t.Fatalf("datagrams still waiting when the request was written = %v, want %d (the drain stops at its bound, the rest is skipped by the wait for the reply)", left, maxDiscarded)
	}
	if left := staleConnOf(c).waiting(); left != 0 {
		t.Fatalf("%d datagrams left after the request was answered, want 0", left)
	}
}
