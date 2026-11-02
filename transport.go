package escp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/davidlukac/escp-go/internal/wire"
)

// datagramConn is the part of the UDP socket that the client uses. It is a *net.UDPConn; tests wrap
// one to make a read fail.
type datagramConn interface {
	ReadFromUDP(b []byte) (int, *net.UDPAddr, error)
	WriteToUDP(b []byte, addr *net.UDPAddr) (int, error)
	SetReadDeadline(t time.Time) error
	LocalAddr() net.Addr
	Close() error
}

var _ datagramConn = (*net.UDPConn)(nil)

// errSilent is what recv reports when no acceptable reply arrived before the deadline (or the
// call was canceled): the retry logic's cue, never returned to a caller.
var errSilent = errors.New("escp: no reply yet")

// errWSAConnReset is the Windows socket error WSAECONNRESET (10054). An unconnected UDP socket reports
// it from a read after a datagram it sent was answered with ICMP "port unreachable"; it says
// nothing about the reply being waited for. Other systems report the same ICMP message as
// ECONNREFUSED, and only when asked to.
const errWSAConnReset syscall.Errno = 10054

// icmpNoise reports whether a read error is only that notice of an earlier send, and the read
// should go on.
func icmpNoise(err error) bool {
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, errWSAConnReset)
}

// maxDatagram is the receive buffer size; ESCP packets are well below it.
const maxDatagram = 4096

// maxFragmentedReply bounds a reply reassembled from fragments. The largest real
// one is the configuration file (a few KiB); the cap keeps a misbehaving or
// spoofed sender from growing the buffer until the deadline.
const maxFragmentedReply = 64 << 10

type reply struct {
	op   byte
	err  ReplyCode
	tlvs []wire.TLV
	// fragment is non-zero when more packets of the same reply follow.
	fragment uint16
}

func (r reply) tlv(t wire.TLVType) ([]byte, bool) {
	for _, x := range r.tlvs {
		if x.Type == t {
			return x.Value, true
		}
	}
	return nil, false
}

// joined is the values of every TLV of the given type in the packet, one after the other.
func (r reply) joined(t wire.TLVType) ([]byte, bool) {
	var out []byte
	found := false
	for _, x := range r.tlvs {
		if x.Type == t {
			out, found = append(out, x.Value...), true
		}
	}
	return out, found
}

func (c *Client) encrypt(b []byte, static bool) []byte {
	if static || c.session == nil {
		return wire.RC4Static(b)
	}
	return wire.RC4Session(b, c.session)
}

func (c *Client) decrypt(b []byte) []byte {
	if p := wire.RC4Static(b); wire.LooksLikeHeader(p) {
		return p
	}
	if c.session != nil {
		return wire.RC4Session(b, c.session)
	}
	return wire.RC4Static(b)
}

// accept decides whether a datagram is the reply to the request in flight: it
// must come from the switch, decode to a valid packet, echo the current
// sequence number and not be our own broadcast discovery coming back.
func (c *Client) accept(from *net.UDPAddr, data []byte) (reply, bool) {
	if !from.IP.Equal(c.dest.IP) && (c.alsoFrom == nil || !from.IP.Equal(c.alsoFrom)) {
		return reply{}, false
	}
	h, t, err := wire.Unmarshal(c.decrypt(data))
	if err != nil || h.Seq != c.seq || h.Op == wire.OpDiscovery {
		return reply{}, false
	}
	if got := MAC(h.SwitchMAC); !c.cfg.MAC.IsZero() && !got.IsZero() && got != c.cfg.MAC {
		// the address answers, but it is another device: remember it and let exchange say so
		c.wrong = &WrongSwitchError{Want: c.cfg.MAC, Got: got, IP: addrOf(from.IP)}
		return reply{}, false
	}
	if !c.haveSwMAC {
		c.swMAC, c.haveSwMAC = h.SwitchMAC, true
	}
	return reply{op: h.Op, err: ReplyCode(h.ErrCode), tlvs: t, fragment: h.Fragment}, true
}

// recv reads packets until one is an acceptable reply to the current request. It returns errSilent
// when the deadline passes first (or the call is canceled), the WrongSwitchError when another
// device answers in place of the switch (waiting longer or resending will not change that), and a
// read error of the socket itself as it is: a failing socket is not a silent switch.
func (c *Client) recv(ctx context.Context, deadline time.Time) (reply, error) {
	buf := make([]byte, maxDatagram)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		_ = c.conn.SetReadDeadline(deadline)
		n, from, err := c.conn.ReadFromUDP(buf)
		switch {
		case errors.Is(err, os.ErrDeadlineExceeded):
			return reply{}, errSilent
		case err != nil && icmpNoise(err):
			continue
		case err != nil:
			return reply{}, fmt.Errorf("escp: receive: %w", err)
		}
		if rep, ok := c.accept(from, buf[:n]); ok {
			return rep, nil
		}
		if c.wrong != nil {
			return reply{}, c.wrong
		}
	}
	return reply{}, errSilent
}

// interruptOnDone makes a read that is waiting for a reply return when ctx is done, instead of
// after the attempt's timeout. The returned function undoes it; call it when the exchange ends.
// Callers hold c.mu and have opened the socket.
//
// The interrupt runs in its own goroutine and may start after the exchange has ended, when the
// next call is already waiting with a deadline of its own: it must not cut that one short. The
// returned function therefore waits for a running interrupt and disarms a late one.
func (c *Client) interruptOnDone(ctx context.Context) func() {
	conn := c.conn // the function may run after Close has dropped the field
	var mu sync.Mutex
	finished := false
	stop := context.AfterFunc(ctx, func() {
		mu.Lock()
		defer mu.Unlock()
		if !finished {
			_ = conn.SetReadDeadline(time.Now())
		}
	})
	return func() {
		stop()
		mu.Lock()
		finished = true
		mu.Unlock()
	}
}

// expired reports why the call must stop: the context was canceled, or its
// deadline has passed. The socket deadline equals the context deadline and can
// fire slightly before the context's own timer does, so the deadline is
// compared directly instead of waiting for ctx.Err() to catch up.
func expired(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if dl, ok := ctx.Deadline(); ok && !time.Now().Before(dl) {
		return context.DeadlineExceeded
	}
	return nil
}

// attemptDeadline is the end of the current attempt: the per-attempt timeout,
// capped by the context deadline.
func (c *Client) attemptDeadline(ctx context.Context) time.Time {
	deadline := time.Now().Add(c.cfg.Timeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		return dl
	}
	return deadline
}

// drainWait is how long discard waits for a datagram that is not there: the last, empty read.
const drainWait = time.Millisecond

// maxDiscarded bounds discard, so a sender that never stops cannot hold a request back.
const maxDiscarded = 64

// discard drops the datagrams already waiting in the socket. A request has not been sent yet, so
// nothing that has arrived can be its reply: it is a late reply to an earlier request or to a
// discovery, or a reply meant for another client, which share the port (see PROTOCOL.md). Left in
// the buffer, one that carries the number the request is about to use would be taken for its reply.
func (c *Client) discard() {
	buf := make([]byte, maxDatagram)
	for range maxDiscarded {
		_ = c.conn.SetReadDeadline(time.Now().Add(drainWait))
		if _, _, err := c.conn.ReadFromUDP(buf); err != nil {
			return
		}
	}
}

// send transmits one request with a fresh sequence number, after dropping what was left in the
// socket.
func (c *Client) send(op byte, tlvs []wire.TLV, static bool, to []*net.UDPAddr) error {
	c.discard()
	c.seq = requestSeq(c.seq, op)
	pkt := c.encrypt(wire.Marshal(wire.Header{Op: op, SwitchMAC: c.swMAC, HostMAC: c.hostMAC, Seq: c.seq}, tlvs), static)
	for _, d := range to {
		if _, err := c.conn.WriteToUDP(pkt, d); err != nil {
			return fmt.Errorf("escp: send: %w", err)
		}
	}
	return nil
}

// exchange sends one packet and waits for the reply with the same sequence
// number from the switch, retrying on silence. Callers hold c.mu.
func (c *Client) exchange(ctx context.Context, op byte, tlvs []wire.TLV, static bool, to []*net.UDPAddr) (reply, error) {
	if err := c.open(ctx); err != nil {
		return reply{}, err
	}
	defer c.interruptOnDone(ctx)()
	c.wrong = nil
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		if err := expired(ctx); err != nil { // a canceled call sends nothing, not even a retry
			return reply{}, err
		}
		if err := c.send(op, tlvs, static, to); err != nil {
			return reply{}, err
		}
		c.log.DebugContext(ctx, "request sent", "op", wire.OpName(op), "seq", c.seq, "attempt", attempt+1, "tlvs", tlvTypes(tlvs), "bytes", tlvBytes(tlvs))
		rep, err := c.recv(ctx, c.attemptDeadline(ctx))
		if err == nil {
			c.log.DebugContext(ctx, "reply received", "op", wire.OpName(rep.op), "seq", c.seq, "error_code", int(rep.err), "tlvs", tlvTypes(rep.tlvs), "bytes", tlvBytes(rep.tlvs))
			return rep, nil
		}
		if !errors.Is(err, errSilent) {
			return reply{}, err
		}
		if err := expired(ctx); err != nil {
			return reply{}, err
		}
		c.log.WarnContext(ctx, "no reply", "op", wire.OpName(op), "seq", c.seq, "attempt", attempt+1, "attempts", c.cfg.Retries+1)
	}
	return reply{}, ErrNoReply
}

// exchangeFragments is exchange for replies that span several packets: it collects the values
// of the TLVs of the given type, one element per packet, until a packet without the fragment
// flag arrives. A missing final fragment restarts the request. The flag says only that more
// packets follow (the header carries no index, no total and no checksum), so nothing here can
// tell that an earlier packet was lost: the caller checks what it gets (see assembleConfig).
func (c *Client) exchangeFragments(ctx context.Context, op byte, tlvs []wire.TLV, typ wire.TLVType) ([][]byte, error) {
	// Callers hold c.mu and run inside withSession, so the socket is open.
	defer c.interruptOnDone(ctx)()
	c.wrong = nil
	for attempt := 0; attempt <= c.cfg.Retries; attempt++ {
		if err := expired(ctx); err != nil {
			return nil, err
		}
		if err := c.send(op, tlvs, false, []*net.UDPAddr{c.dest}); err != nil {
			return nil, err
		}
		c.log.DebugContext(ctx, "request sent", "op", wire.OpName(op), "seq", c.seq, "attempt", attempt+1, "tlvs", tlvTypes(tlvs), "bytes", tlvBytes(tlvs))
		got, err := c.collectFragments(ctx, typ)
		if err != nil {
			return nil, err
		}
		if got.complete {
			return got.parts, nil
		}
		if err := expired(ctx); err != nil {
			return nil, err
		}
		c.log.WarnContext(ctx, "final fragment missing; asking again", "seq", c.seq, "attempt", attempt+1, "attempts", c.cfg.Retries+1, "packets", len(got.parts), "bytes", got.bytes)
	}
	return nil, ErrNoReply
}

// fragments is what one attempt of exchangeFragments collected.
type fragments struct {
	parts    [][]byte // the values of the wanted TLV, one per packet
	bytes    int      // their total length
	complete bool     // a packet without the fragment flag arrived
}

// collectFragments reads the packets of one reply until the last one arrives or the switch goes
// silent (then the result is incomplete). A refusal by the switch and a reply larger than any real
// one are errors.
func (c *Client) collectFragments(ctx context.Context, typ wire.TLVType) (fragments, error) {
	var got fragments
	for {
		rep, err := c.recv(ctx, c.attemptDeadline(ctx))
		if errors.Is(err, errSilent) {
			return got, nil
		}
		if err != nil {
			return got, err
		}
		if rep.err != 0 {
			return got, &RejectedError{Code: rep.err}
		}
		if part, found := rep.joined(typ); found {
			got.parts = append(got.parts, part)
			got.bytes += len(part)
			c.log.DebugContext(ctx, "fragment received", "seq", c.seq, "packet", len(got.parts), "bytes", len(part), "more", rep.fragment != 0)
		}
		if got.bytes > maxFragmentedReply {
			return got, ErrReplyTooLarge
		}
		if rep.fragment == 0 {
			got.complete = true
			return got, nil
		}
	}
}

func (c *Client) discoverTargets() []*net.UDPAddr {
	return []*net.UDPAddr{c.dest, {IP: net.IP(c.cfg.BroadcastAddr.AsSlice()), Port: c.cfg.SwitchPort}}
}
