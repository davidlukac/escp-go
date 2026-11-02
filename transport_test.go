package escp

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
)

func TestReplyTLVLookup(t *testing.T) {
	r := reply{tlvs: []wire.TLV{{Type: 5, Value: []byte{1}}, {Type: 9, Value: []byte{2, 3}}}}
	if v, ok := r.tlv(9); !ok || len(v) != 2 || v[1] != 3 {
		t.Fatalf("existing TLV not found: %v %v", v, ok)
	}
	if v, ok := r.tlv(7); ok || v != nil {
		t.Fatalf("missing TLV must report not found, got %v %v", v, ok)
	}
}

func TestDecryptChoosesTheKeyThatYieldsAHeader(t *testing.T) {
	pkt := wire.Marshal(wire.Header{Op: wire.OpReply, Seq: 4}, nil)
	garbage := []byte("this is definitely not an ESCP packet, and long enough to look like one....")

	noSession := &Client{}
	if got := noSession.decrypt(wire.RC4Static(pkt)); !wire.LooksLikeHeader(got) {
		t.Fatal("a statically encrypted packet must decode without a session")
	}
	if got := noSession.decrypt(garbage); string(got) != string(wire.RC4Static(garbage)) {
		t.Fatal("without a session, undecodable data falls back to the static key")
	}

	key := []byte("AbCdEf12")
	withSession := &Client{session: key}
	if got := withSession.decrypt(wire.RC4Session(pkt, key)); !wire.LooksLikeHeader(got) {
		t.Fatal("a session-encrypted packet must decode with the session key")
	}
	if got := withSession.decrypt(wire.RC4Static(pkt)); !wire.LooksLikeHeader(got) {
		t.Fatal("a statically encrypted packet must still decode while a session exists")
	}
}

func TestAcceptFiltersForeignAndStaleDatagrams(t *testing.T) {
	c, err := Dial(Config{Host: "127.0.0.1", SwitchPort: 1})
	if err != nil {
		t.Fatal(err)
	}
	c.seq = 41
	fromSwitch := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
	enc := func(op byte, seq uint16, tlvs ...wire.TLV) []byte {
		return wire.RC4Static(wire.Marshal(wire.Header{Op: op, Seq: seq, ErrCode: 3, SwitchMAC: [6]byte{1, 2, 3, 4, 5, 6}}, tlvs))
	}
	tests := []struct {
		name string
		from *net.UDPAddr
		data []byte
		ok   bool
	}{
		{"other source address", &net.UDPAddr{IP: net.IPv4(10, 9, 8, 7)}, enc(wire.OpReply, 41), false},
		{"not a packet", fromSwitch, []byte("garbage"), false},
		{"stale sequence number", fromSwitch, enc(wire.OpReply, 40), false},
		{"our own discovery echoed back", fromSwitch, enc(wire.OpDiscovery, 41), false},
		{"the awaited reply", fromSwitch, enc(wire.OpReply, 41, wire.TLV{Type: 9, Value: []byte{7}}), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep, ok := c.accept(tt.from, tt.data)
			if ok != tt.ok {
				t.Fatalf("accept = %v, want %v", ok, tt.ok)
			}
			if ok && (rep.op != wire.OpReply || rep.err != 3 || len(rep.tlvs) != 1 || rep.tlvs[0].Value[0] != 7) {
				t.Fatalf("decoded reply wrong: %+v", rep)
			}
		})
	}
	if !c.haveSwMAC || c.swMAC != [6]byte{1, 2, 3, 4, 5, 6} {
		t.Fatalf("the switch MAC must be learned from the first accepted reply, got %x (have=%v)", c.swMAC, c.haveSwMAC)
	}
	// a later reply with another MAC must not overwrite what we learned
	c.accept(fromSwitch, wire.RC4Static(wire.Marshal(wire.Header{Op: wire.OpReply, Seq: 41, SwitchMAC: [6]byte{9, 9, 9, 9, 9, 9}}, nil)))
	if c.swMAC != [6]byte{1, 2, 3, 4, 5, 6} {
		t.Fatalf("learned MAC was overwritten: %x", c.swMAC)
	}
}

func TestSendFailureIsReported(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	if err := c.open(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.conn.Close() // sabotage the socket behind the client's back
	_, err := c.Info(context.Background())
	if err == nil || !strings.Contains(err.Error(), "escp: send") {
		t.Fatalf("want a send error, got %v", err)
	}
}

func TestContextDeadlineBeatsTheConfiguredTimeout(t *testing.T) {
	f := escptest.Start(t)
	f.Mute()
	c := newBoundClient(t, Config{Host: "127.0.0.1", Timeout: 5 * time.Second, SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback()})
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Info(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want context.DeadlineExceeded, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("the 150ms context deadline was ignored, request took %v", elapsed)
	}
}

func TestCanceledContextStopsRetrying(t *testing.T) {
	f := escptest.Start(t)
	f.Mute()
	c := dialFast(t, f, "secret")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Info(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestRetryOnLostPacket(t *testing.T) {
	f := escptest.Start(t)
	f.DropFirst(2) // lose both datagrams of the first attempt (the probe goes to the switch and to the broadcast address)
	c := dialFake(t, f, "secret")
	if _, err := c.Info(context.Background()); err != nil {
		t.Fatalf("expected retry to recover: %v", err)
	}
}

func TestNoReply(t *testing.T) {
	f := escptest.Start(t)
	f.Mute()
	c := dialFake(t, f, "secret")
	c.cfg.Retries = 1
	if _, err := c.Info(context.Background()); !errors.Is(err, ErrNoReply) {
		t.Fatalf("want ErrNoReply, got %v", err)
	}
}

func TestExpiredReportsCancellationAndPassedDeadlines(t *testing.T) {
	if err := expired(context.Background()); err != nil {
		t.Fatalf("a context without a deadline is not expired: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	if err := expired(ctx); err != nil {
		t.Fatalf("a deadline in the future is not expired: %v", err)
	}
	cancel()
	if err := expired(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("a canceled context must report cancellation, got %v", err)
	}
}

// The deadline can pass before the context's own timer has marked it done.
// expired must still report it, which is what makes the callers stop retrying.
func TestExpiredNoticesADeadlineThatThePassedTimerHasNotReportedYet(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Millisecond))
	defer cancel()
	if err := expired(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a deadline in the past must be reported as exceeded, got %v", err)
	}
}

// deadlineOnlyContext reports a deadline in the past while Err is still nil,
// the window in which the socket deadline has fired but the context's own timer
// has not. That window is real but microscopic, so it is built by hand to cover
// the branch deterministically instead of by timing.
type deadlineOnlyContext struct{ context.Context }

func (deadlineOnlyContext) Deadline() (time.Time, bool) {
	return time.Now().Add(-time.Second), true
}

func (deadlineOnlyContext) Err() error { return nil }

func TestExpiredComparesTheDeadlineWhenTheContextHasNotNoticedYet(t *testing.T) {
	err := expired(deadlineOnlyContext{context.Background()})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a passed deadline with a nil Err must be reported as exceeded, got %v", err)
	}
}

func TestSendFailureOnClosedSocketAffectsReadsAndBackup(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	if _, err := c.LED(context.Background()); err != nil { // opens the socket and establishes the session
		t.Fatal(err)
	}
	if err := c.conn.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Info(context.Background()); err == nil || !strings.Contains(err.Error(), "send") {
		t.Fatalf("want a send error on a closed socket, got %v", err)
	}
	if _, err := c.PortStats(context.Background()); err == nil {
		t.Fatal("a read on a closed socket must fail")
	}
	if _, err := c.ConfigBackup(context.Background()); err == nil {
		t.Fatal("a backup on a closed socket must fail")
	}
}

func TestBindFailureIsReported(t *testing.T) {
	taken, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	port := localPort(t, taken)
	c, err := Dial(Config{Host: "127.0.0.1", HostPort: port, Timeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Info(context.Background()); err == nil || !strings.Contains(err.Error(), "binding UDP port") {
		t.Fatalf("want a bind error naming the port, got %v", err)
	}
	if _, err := c.ConfigBackup(context.Background()); err == nil {
		t.Fatal("backup must fail when the socket cannot be opened")
	}
}

// An attempt whose deadline has already passed must give up at once instead of
// blocking on the socket: no reply, and no waiting.
// A canceled call does not wait for the attempt's deadline, even when that is some way off.
func TestRecvWithACanceledContextReturnsImmediately(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	if err := c.open(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, err := c.recv(ctx, time.Now().Add(5*time.Second)); !errors.Is(err, errSilent) {
		t.Fatalf("nothing was sent, so nothing can have been received: got %v", err)
	}
	if took := time.Since(start); took > 2*time.Second { // the deadline is 5 s away
		t.Fatalf("recv waited %v for a canceled call", took)
	}
}

func TestRecvWithExpiredDeadlineReturnsImmediately(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	if err := c.open(context.Background()); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if rep, err := c.recv(context.Background(), time.Now().Add(-time.Second)); !errors.Is(err, errSilent) || rep.op != 0 || len(rep.tlvs) != 0 {
		t.Fatalf("expected no reply, got %+v, %v", rep, err)
	}
	if took := time.Since(start); took > 100*time.Millisecond {
		t.Fatalf("recv blocked for %v although the deadline had passed", took)
	}
}

// readFaultConn is a socket whose reads fail on request: right after a datagram has been sent
// (so the clean-up read that precedes every send is not affected), the next read returns err, once
// or, when sticky, for good.
type readFaultConn struct {
	datagramConn
	err     error
	sticky  bool
	pending bool
	failed  int // reads that returned err
	healthy int // requests sent before the fault arms
}

func (r *readFaultConn) WriteToUDP(b []byte, addr *net.UDPAddr) (int, error) {
	if r.healthy > 0 {
		r.healthy--
	} else {
		r.pending = true
	}
	return r.datagramConn.WriteToUDP(b, addr)
}

func (r *readFaultConn) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	if r.pending {
		r.pending = r.sticky
		r.failed++
		return 0, nil, r.err
	}
	return r.datagramConn.ReadFromUDP(b)
}

// A socket that fails is reported as what it is, at once, and not as a switch that stays silent after
// every retry.
func TestASocketReadErrorIsReportedAsItIsAndNotRetried(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	if err := c.open(context.Background()); err != nil {
		t.Fatal(err)
	}
	broken := errors.New("the network went away")
	conn := &readFaultConn{datagramConn: c.conn, err: &net.OpError{Op: "read", Net: "udp", Err: broken}, sticky: true}
	c.conn = conn
	_, err := c.Info(context.Background())
	if !errors.Is(err, broken) || errors.Is(err, ErrNoReply) || !strings.Contains(err.Error(), "escp: receive") {
		t.Fatalf("got %v, want the socket's own error behind %q", err, "escp: receive")
	}
	if conn.failed != 1 {
		t.Fatalf("%d reads failed, want one: a failing socket ends the call and is not retried", conn.failed)
	}
}

// The same holds while a reply is downloaded in fragments: the key exchange succeeds, the download fails.
func TestASocketReadErrorDuringADownloadIsReportedAsItIs(t *testing.T) {
	f := escptest.Start(t)
	c := dialFake(t, f, "secret")
	if _, err := c.Info(context.Background()); err != nil { // the switch's MAC is known: one packet per request
		t.Fatal(err)
	}
	broken := errors.New("the network went away")
	c.conn = &readFaultConn{datagramConn: c.conn, err: broken, sticky: true, healthy: 1}
	if _, err := c.ConfigBackup(context.Background()); !errors.Is(err, broken) {
		t.Fatalf("got %v, want the socket's own error", err)
	}
}

// The notice of an earlier send that went to a closed port (ICMP port unreachable) is no answer and no
// failure: the read goes on, and the reply that follows is taken.
func TestAnIcmpNoticeInTheSocketDoesNotEndTheWait(t *testing.T) {
	for name, noise := range map[string]error{
		"ECONNREFUSED (Linux, macOS)": &net.OpError{Op: "read", Net: "udp", Err: os.NewSyscallError("recvfrom", syscall.ECONNREFUSED)},
		"WSAECONNRESET (Windows)":     &net.OpError{Op: "read", Net: "udp", Err: os.NewSyscallError("wsarecvfrom", errWSAConnReset)},
	} {
		t.Run(name, func(t *testing.T) {
			f := escptest.Start(t)
			c := dialFake(t, f, "secret")
			if err := c.open(context.Background()); err != nil {
				t.Fatal(err)
			}
			c.conn = &readFaultConn{datagramConn: c.conn, err: noise}
			if _, err := c.Info(context.Background()); err != nil {
				t.Fatalf("Info = %v, want the reply that followed the notice", err)
			}
		})
	}
}

// Another device answering in the middle of a download is a wrong switch, not a silent one.
func TestAnotherDeviceAnsweringDuringADownloadIsAWrongSwitch(t *testing.T) {
	f := escptest.Start(t)
	c, _ := dialMAC(t, f, "127.0.0.1", wantMAC, nil)
	if _, err := c.Info(context.Background()); err != nil { // the session exists, the identity is checked
		t.Fatal(err)
	}
	f.SetMAC([6]byte(otherMAC)) // the address now belongs to another device
	_, err := c.ConfigBackup(context.Background())
	var wrong *WrongSwitchError
	if !errors.Is(err, ErrWrongSwitch) || !errors.As(err, &wrong) || wrong.Got != otherMAC {
		t.Fatalf("got %v, want ErrWrongSwitch naming %x", err, otherMAC)
	}
}

func TestIcmpNoiseIsOnlyTheTwoNotices(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want bool
	}{
		{syscall.ECONNREFUSED, true},
		{errWSAConnReset, true},
		{&net.OpError{Err: os.NewSyscallError("recvfrom", syscall.ECONNREFUSED)}, true},
		{syscall.EBADF, false},
		{net.ErrClosed, false},
		{os.ErrDeadlineExceeded, false},
		{errors.New("other"), false},
	} {
		if got := icmpNoise(tc.err); got != tc.want {
			t.Errorf("icmpNoise(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}
