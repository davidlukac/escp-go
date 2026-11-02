package escptest

import (
	"math/big"
	"net"
	"sync"
	"testing"

	"github.com/davidlukac/escp-go/internal/wire"
)

// Credentials the switch accepts.
const (
	Username = "admin"
	Password = "secret"
)

// DefaultFirmware is the firmware version the switch reports until SetFirmware changes it: the
// older of the two builds the client is verified on. The rules of EnableModel were measured on
// 20250710, and on 20230218 where a comment says so.
const DefaultFirmware = "1.0.0 Build 20230218 Rel.50633"

// Switch is the fake. Create it with Start; change its behavior with Configure.
// It can decrypt the session key because the switch's 64-bit RSA modulus factors
// trivially (p=3443843009, q=3765816959), which is also why ESCP offers no real secrecy.
type Switch struct {
	conn     *net.UDPConn
	port     int // the UDP port conn is bound to
	user     string
	password string

	mu               sync.Mutex
	hostname         string
	history          [][]wire.TLV    // every accepted SET, in order
	lastSet          []wire.TLV      // settings TLVs (credentials stripped) of the most recent accepted SET
	portSets         map[byte][]byte // the last port row SET addressed to each port
	setIP            []byte
	mac              [6]byte                 // zero means the default address
	staleDiscoveries int                     // the next n discoveries still report 127.0.0.9, like a network whose caches have not caught up
	addr             map[wire.TLVType][]byte // IP, mask and gateway as last set; discovery reports them instead of the defaults
	led              bool
	firmware         string
	session          []byte
	dropNth          int // drop the nth received packet (1-based) to test retries
	dropFirst        int // drop the first n received packets
	droppedSets      int // authenticated SETs ignored under DropSets
	recv             int
	mute             bool // swallow everything (switch gone)
	backupMode       BackupMode
	backupScript     []BackupMode                    // consumed one per backup request before backupMode applies
	departed         bool                            // a system SET was applied under OnSystemSet and the switch has left
	stateful         bool                            // EnableState was called: SETs are remembered and answered by later GETs
	store            map[wire.TLVType][]byte         // the last value of each settings TLV written (or seeded) under EnableState
	model            bool                            // EnableModel was called: cross-setting rules apply
	lease            *Lease                          // what the switch is given when its DHCP flag goes on (see Lease)
	dhcp             bool                            // the switch leases its address
	acquiring        int                             // packets still swallowed while the modeled switch asks for a lease
	bundles          map[wire.TLVType][]wire.TLVType // requests that the switch answers with more types than asked for
	rows             map[wire.TLVType][][]byte       // tables with one TLV per row (ports, priorities, rates), keyed by the first byte of a row

	faults Faults // see Configure
}

var (
	fakeP = big.NewInt(3443843009)
	fakeQ = big.NewInt(3765816959)
)

// Start runs a fake switch on a free loopback UDP port until the test ends. The LED starts on.
func Start(tb testing.TB) *Switch {
	tb.Helper()
	f, err := Listen()
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = f.Close() }) // best effort at the end of the test
	return f
}

// Listen is Start for code that is not a test, such as an example: it runs a fake switch on a
// free loopback UDP port and returns it; the caller closes it. The LED starts on.
func Listen() (*Switch, error) {
	conn, port, err := listenUnreserved(listenLoopback)
	if err != nil {
		return nil, err
	}
	lease := DefaultLease()
	f := &Switch{conn: conn, port: port, user: Username, password: Password, led: true, firmware: DefaultFirmware, lease: &lease}
	go f.serve()
	return f, nil
}

// Close stops the switch. Requests that arrive afterwards get no answer.
func (f *Switch) Close() error { return f.conn.Close() }

// UDPPort is the UDP port the switch listens on; give it to the client as its switch port.
func (f *Switch) UDPPort() int { return f.port }

// TLVType is the type number of a Setting: the numbers of PROTOCOL.md. It is named here so that a
// test outside the escp module can write the func literals the fake takes (Faults.OnGet) without
// importing the client's internals; a plain number is a valid TLVType wherever a constant is
// expected.
type TLVType = wire.TLVType

func (f *Switch) serve() {
	buf := make([]byte, 4096)
	for {
		n, from, err := f.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		f.mu.Lock()
		f.recv++
		drop := f.mute || f.recv == f.dropNth || f.recv <= f.dropFirst
		if f.acquiring > 0 {
			f.acquiring--
			drop = true
		}
		f.mu.Unlock()
		if drop {
			continue
		}
		raw := append([]byte(nil), buf[:n]...)
		pkt := wire.RC4Static(raw)
		viaSession := false
		if !wire.LooksLikeHeader(pkt) {
			f.mu.Lock()
			s := f.session
			f.mu.Unlock()
			if s == nil {
				continue
			}
			pkt = wire.RC4Session(raw, s)
			viaSession = true
		}
		h, tlvs, err := wire.Unmarshal(pkt)
		if err != nil {
			continue
		}
		f.handle(request{from: from, h: h, tlvs: tlvs, viaSession: viaSession})
	}
}

// request is a packet the switch received, decoded, with where it came from and how it was
// encrypted.
type request struct {
	from *net.UDPAddr
	h    wire.Header
	tlvs []wire.TLV
	// viaSession says it was encrypted with the session key.
	viaSession bool
}

// find returns the value of the first TLV of the given type.
func (r request) find(t wire.TLVType) ([]byte, bool) {
	for _, x := range r.tlvs {
		if x.Type == t {
			return x.Value, true
		}
	}
	return nil, false
}

// has reports whether the request carries a TLV of the given type.
func (r request) has(t wire.TLVType) bool {
	_, ok := r.find(t)
	return ok
}

// reply answers the request with the given operation, code and TLVs; useSession says whether the
// reply travels under the session key (the replies to discovery do not).
func (f *Switch) reply(r request, op byte, code int32, tlvs []wire.TLV, useSession bool) {
	h := r.h
	h.Op, h.ErrCode = op, code
	f.mu.Lock()
	h.SwitchMAC = f.macAddr()
	s := f.session
	f.mu.Unlock()
	b := wire.Marshal(h, tlvs)
	if useSession && s != nil {
		b = wire.RC4Session(b, s)
	} else {
		b = wire.RC4Static(b)
	}
	_, _ = f.conn.WriteToUDP(b, r.from)
}
