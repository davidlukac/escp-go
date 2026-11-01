package escp

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/davidlukac/escp-go/internal/udp"
)

// Client talks ESCP to one switch. It is safe for concurrent use; requests
// are serialized because the switch answers one request at a time. Use one Client per
// switch: two Clients for the same switch were measured to fail completely, apparently
// because the switch keeps a single session (see the package documentation).
type Client struct {
	cfg  Config
	mu   sync.Mutex
	conn *net.UDPConn
	dest *net.UDPAddr
	// alsoFrom is a second address replies are accepted from while the switch is
	// being moved to it (SetIPConfig); nil otherwise.
	alsoFrom net.IP
	// moveWait overrides ipMoveWait (tests); zero means the default.
	moveWait time.Duration
	// allowLoopback lets tests move a fake switch on 127.0.0.0/8 (tests only).
	allowLoopback bool

	hostMAC, swMAC [6]byte
	haveSwMAC      bool
	seq            uint16
	session        []byte
	// switchPorts is the switch's number of ports, read on the first write that names a port
	// (zero until then) and kept: it is hardware. Guarded by mu.
	switchPorts int
	// groupLimit is the highest port-based VLAN group id the switch has, read on the first group
	// write (unknown until then) and kept: it is hardware. Guarded by mu.
	groupLimit PortVLANLimit
	// wrong is the device that answered the request in flight with a MAC other than Config.MAC.
	wrong *WrongSwitchError
	// locator finds a switch by MAC address; tests replace it.
	locator func(ctx context.Context, mac MAC) (Info, error)
	// log is Config.Logger, or a logger that discards, with the switch's host and MAC attached.
	log *slog.Logger
}

// Dial prepares a client for the switch at cfg.Host, which must be an IPv4 address or a
// name that resolves to one (a name is looked up now, and the lookup cannot be canceled), or,
// when cfg.MAC is set, may be left empty to find the switch by its MAC address on the first
// request. A configuration that Config.Validate refuses is refused here, with an error that
// matches ErrInvalid, and not on the first call. It sends nothing to the switch and
// binds no socket: the local UDP port is bound on first use, so merely configuring a client
// (a program that builds one and validates its input) has no side effects on the switch. Close the client
// when done.
func Dial(cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 3 * time.Second
	}
	switch {
	case cfg.Retries == 0:
		cfg.Retries = defaultRetries
	case cfg.Retries < 0:
		cfg.Retries = 0
	}
	if cfg.SwitchPort == 0 {
		cfg.SwitchPort = defaultSwitchPort
	}
	if cfg.HostPort == 0 {
		cfg.HostPort = defaultHostPort
	}
	if !cfg.BroadcastAddr.IsValid() {
		cfg.BroadcastAddr = netip.AddrFrom4([net.IPv4len]byte{255, 255, 255, 255})
	}
	base := clientLogger(cfg)
	c := &Client{cfg: cfg, log: identified(base)}
	c.locator = hostLocator(base) // Locate identifies itself
	if cfg.Host == "" && !cfg.MAC.IsZero() {
		// no address yet: the first request finds the switch by its MAC address
		c.dest = &net.UDPAddr{Port: cfg.SwitchPort}
	} else {
		ip, err := net.ResolveIPAddr("ip4", cfg.Host)
		if err != nil {
			return nil, fmt.Errorf("escp: resolving %q: %w", cfg.Host, err)
		}
		c.dest = &net.UDPAddr{IP: ip.IP, Port: cfg.SwitchPort}
		c.hostMAC = localMAC(ip.IP)
	}
	var n [2]byte
	_, _ = rand.Read(n[:])
	c.seq = startSeq(n)
	return c, nil
}

// Close releases the UDP socket, if it was opened, so that another client or Discover can
// use the reply port. A closed client opens the socket again if it is used afterwards.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

func (c *Client) open(ctx context.Context) error {
	if c.conn != nil {
		return nil
	}
	conn, err := bindReplyPort(ctx, c.cfg.HostPort)
	if err != nil {
		return err
	}
	c.conn = conn
	return nil
}

// bindReplyPort binds the UDP port the switches reply to, sharing it with the other clients of the
// host (see PROTOCOL.md). Client and Discover bind it the same way, so a failure reads the same.
func bindReplyPort(ctx context.Context, port int) (*net.UDPConn, error) {
	conn, err := udp.Listen(ctx, port)
	if err != nil {
		return nil, fmt.Errorf("escp: binding UDP port %d (is a client that does not share the port running, such as an old Easy Smart utility?): %w", port, err)
	}
	return conn, nil
}

// discardPort is the UDP "discard" port: localMAC only needs the socket to learn which local address a
// packet to dst would leave from, and sends nothing.
const discardPort = 9

// localMAC returns the MAC of the interface used to reach dst, or zero.
func localMAC(dst net.IP) [6]byte {
	probe, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: dst, Port: discardPort})
	if err != nil {
		return [6]byte{}
	}
	defer func() { _ = probe.Close() }() // best-effort: the probe socket only reveals the local address
	ifs, _ := net.Interfaces()
	return macHolding(localIP(probe), ifs, func(i net.Interface) ([]net.Addr, error) { return i.Addrs() })
}

// localIP is the local address of a UDP connection, nil for a connection that is not one.
func localIP(c net.Conn) net.IP {
	if a, ok := c.LocalAddr().(*net.UDPAddr); ok {
		return a.IP
	}
	return nil
}

// macHolding returns the hardware address of the first interface of ifs that has a six-byte hardware
// address and holds addr among the addresses addrs lists for it; zero when there is none. An interface
// whose addresses cannot be listed is skipped.
func macHolding(addr net.IP, ifs []net.Interface, addrs func(net.Interface) ([]net.Addr, error)) [6]byte {
	for _, ifc := range ifs {
		if len(ifc.HardwareAddr) != macLen {
			continue
		}
		list, err := addrs(ifc)
		if err != nil {
			continue
		}
		for _, a := range list {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.Equal(addr) {
				return [6]byte(ifc.HardwareAddr)
			}
		}
	}
	return [6]byte{}
}

// hostLocator is the lookup a client uses: Locate on the interfaces of this host, logging to the
// client's logger (before it is identified: Locate does that).
func hostLocator(log *slog.Logger) func(ctx context.Context, mac MAC) (Info, error) {
	return func(ctx context.Context, mac MAC) (Info, error) {
		return Locate(ctx, mac, DiscoverOptions{Logger: log})
	}
}

// clientLogger is Config.Logger (discarding when nil) with the switch the client talks to attached,
// so the lines of several clients in one log can be told apart. The caller identifies it.
func clientLogger(cfg Config) *slog.Logger {
	l := orDiscard(cfg.Logger)
	if cfg.Host != "" {
		l = l.With("host", cfg.Host)
	}
	if !cfg.MAC.IsZero() {
		l = l.With("mac", cfg.MAC.String())
	}
	return l
}
