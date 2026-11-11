package escp

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/davidlukac/escp-go/internal/udp"
	"github.com/davidlukac/escp-go/model"
)

// Client talks ESCP to one switch. It is safe for concurrent use; requests
// are serialized because the switch answers one request at a time, and a call that
// waits its turn gives up when its context ends. Use one Client per
// switch: two Clients for the same switch were measured to fail completely, apparently
// because the switch keeps a single session (see the package documentation). Within one process
// this is enforced: Dial refuses a second client of a switch that another client has not Closed
// (ErrSwitchInUse). A client dropped without Close gives its claim up when it is garbage collected.
// Other processes cannot be seen, and stay the caller's rule.
type Client struct {
	cfg Config
	mu  sync.Mutex
	// modify serializes the calls that read a setting, change part of it and write it all back
	// (see beginModify). It is taken before mu and never while holding it.
	modify sync.Mutex
	conn   datagramConn
	dest   *net.UDPAddr
	// alsoFrom is a second address replies are accepted from while the switch is
	// being moved to it (SetIPConfig); nil otherwise.
	alsoFrom net.IP

	hostMAC model.MAC
	// ident is who the client believes the switch is (the pin, the MAC of the first contact).
	ident   identity
	seq     uint16
	session []byte
	// switchPorts is the switch's number of ports, read on the first write that names a port
	// (zero until then) and kept: it is hardware. Atomic, so that a call reads it without the
	// lock another call may be holding for a whole exchange.
	switchPorts atomic.Int64
	// groupLimit is the highest port-based VLAN group id the switch has (a PortVLANLimit), read on
	// the first group write (unknown until then) and kept: it is hardware. Atomic like switchPorts.
	groupLimit atomic.Int64
	// claim is what the client holds in the registry of switches in use (registry.go); nil until
	// the client claims its switch.
	claim *claim
	// closed is set by Close and never cleared; guarded by mu.
	closed bool
	// stop is closed by Close (once, see stopOnce) before it waits for the lock, so that the call in
	// flight ends at once and the calls queued behind it give up. Nil in a Client that was not made
	// by Dial: the zero Client, which is closed.
	stop     chan struct{}
	stopOnce sync.Once
	// seams are the knobs only tests turn; Dial sets them to their defaults.
	seams
	// log is Config.Logger, or a logger that discards, with the switch's host and MAC attached.
	log *slog.Logger
}

// seams are what the tests of the client replace or shorten, kept apart from the state it needs to
// work. A test sets them through a dialOption, or directly when the value needs the client itself.
type seams struct {
	// moveWait overrides ipMoveWait; zero means the default.
	moveWait time.Duration
	// movePoll overrides ipMovePoll, the pause between probes while waiting for a switch to move
	// or to lease an address; zero means the default.
	movePoll time.Duration
	// firstContact overrides firstContactBudget, the time the configured address gets to answer
	// the first discovery of a client pinned by MAC; zero means the default.
	firstContact time.Duration
	// allowLoopback lets a test move a fake switch on 127.0.0.0/8.
	allowLoopback bool
	// shared lets a test put several clients on one fake switch: the client claims nothing. It is
	// the default in this package's tests (claimsByDefault); withClaim turns it off.
	shared bool
	// locator finds a switch by MAC address; the default is Locate on the interfaces of this host.
	locator func(ctx context.Context, mac model.MAC) (model.Info, error)
	// clock replaces time.Now where a test must decide how much time has passed (the drain);
	// nil means time.Now.
	clock func() time.Time
}

// now is the current time, from the clock seam when a test set one.
func (s seams) now() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now()
}

// dialOption changes how dial builds a client; only tests use it.
type dialOption func(*Client)

// withLoopback lets the client move a switch that lives on 127.0.0.0/8.
func withLoopback() dialOption { return func(c *Client) { c.allowLoopback = true } }

// withClaim makes this client claim its switch even in the tests, which otherwise do not.
func withClaim() dialOption { return func(c *Client) { c.shared = false } }

// withMoveWait shortens the wait for a switch that was told to move.
func withMoveWait(d time.Duration) dialOption { return func(c *Client) { c.moveWait = d } }

// withMovePoll shortens the pause between the probes of that wait.
func withMovePoll(d time.Duration) dialOption { return func(c *Client) { c.movePoll = d } }

// withFirstContact shortens the time the configured address gets to answer the first discovery.
func withFirstContact(d time.Duration) dialOption { return func(c *Client) { c.firstContact = d } }

// withLocator replaces the lookup of a switch by its MAC address.
func withLocator(f func(ctx context.Context, mac model.MAC) (model.Info, error)) dialOption {
	return func(c *Client) { c.locator = f }
}

// Dial prepares a client for the switch at cfg.Host, which must be an IPv4 address or a
// name that resolves to one (a name is looked up now; use DialContext to bound or cancel the lookup), or,
// when cfg.MAC is set, may be left empty to find the switch by its MAC address on the first
// request. A configuration that Config.Validate refuses is refused here, with an error that
// matches ErrInvalid, and not on the first call. It sends nothing to the switch and
// binds no socket: the local UDP port is bound on first use, so merely configuring a client
// (a program that builds one and validates its input) has no side effects on the switch. Close the client
// when done.
func Dial(cfg Config) (*Client, error) {
	return DialContext(context.Background(), cfg)
}

// DialContext is Dial with a context for the one step that can wait: the lookup of a host name.
// It returns the context's error (wrapped, so errors.Is matches context.Canceled and
// context.DeadlineExceeded) when ctx ends during the lookup. The returned client does not keep ctx:
// it is no deadline for later calls, which take their own context. A literal IPv4 address and the
// MAC-only form involve no lookup, so ctx does not matter for them.
func DialContext(ctx context.Context, cfg Config) (*Client, error) {
	return dial(ctx, net.DefaultResolver, cfg)
}

// ipLookuper is the part of *net.Resolver that Dial needs; tests replace it.
type ipLookuper interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// resolveIPv4 returns the first IPv4 address host stands for: the address itself for a literal one,
// else the first IPv4 address the lookup returns. A host with no IPv4 address is an error.
func resolveIPv4(ctx context.Context, r ipLookuper, host string) (net.IP, error) {
	addrs, err := r.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, a := range addrs {
		if ip := a.IP.To4(); ip != nil {
			return ip, nil
		}
	}
	return nil, errNoIPv4
}

// errNoIPv4 is the cause of a host name that resolves, but not to an IPv4 address.
var errNoIPv4 = errors.New("no IPv4 address")

func dial(ctx context.Context, r ipLookuper, cfg Config, opts ...dialOption) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 3 * time.Second
	}
	switch {
	case cfg.Retries < 0:
		cfg.Retries = 0
	case cfg.Retries == 0:
		cfg.Retries = defaultRetries
	}
	if cfg.SwitchPort == 0 {
		cfg.SwitchPort = defaultSwitchPort
	}
	if cfg.HostPort == 0 {
		cfg.HostPort = defaultHostPort
	}
	if !cfg.BroadcastAddr.IsValid() {
		cfg.BroadcastAddr = limitedBroadcast
	}
	base := clientLogger(cfg)
	c := &Client{cfg: cfg, log: identified(base), ident: identity{pin: cfg.MAC}, seams: seams{shared: !claimsByDefault}, stop: make(chan struct{})}
	c.locator = hostLocator(base, cfg) // Locate identifies itself
	if cfg.Host == "" && !cfg.MAC.IsZero() {
		// no address yet: the first request finds the switch by its MAC address
		c.dest = &net.UDPAddr{Port: cfg.SwitchPort}
	} else {
		ip, err := resolveIPv4(ctx, r, cfg.Host)
		if err != nil {
			return nil, fmt.Errorf("escp: resolving %q: %w", cfg.Host, err)
		}
		// The text check of Validate only knows the dotted form; the system resolver also reads
		// "4294967295" or "0xffffffff", and a hosts file or DNS can map a name to anything.
		if a := addrOf(ip); namesMoreThanOneDevice(a) {
			return nil, invalidf("escp: Host %q resolves to %s, which does not name one switch (an unspecified, multicast or broadcast address)", cfg.Host, a)
		}
		c.dest = &net.UDPAddr{IP: ip, Port: cfg.SwitchPort}
		c.hostMAC = localMAC(ip)
	}
	var n [2]byte
	_, _ = rand.Read(n[:])
	c.seq = startSeq(n)
	for _, o := range opts {
		o(c)
	}
	if err := c.claimSwitch(); err != nil {
		return nil, err
	}
	return c, nil
}

// Close ends the client for good: it releases the UDP socket, if it was opened, so that another
// client or Discover can use the reply port, and ends the client's claim on its switch, so that
// another client can be made for it (ErrSwitchInUse). Every later call of the client returns
// ErrClosed and sends nothing; make a new client with Dial. Close ends the call in flight (it
// returns ErrClosed, a write it was sending may have been applied) and the calls waiting behind it
// instead of waiting for them, so it returns in moments; a lookup of the switch by its MAC that is
// running ends within its own window (two seconds at most). It is safe to call more than once.
func (c *Client) Close() error {
	if c == nil || c.stop == nil {
		return nil // the zero Client is closed already
	}
	c.stopOnce.Do(func() { close(c.stop) })
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.claim != nil {
		c.claim.release()
	}
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
func localMAC(dst net.IP) model.MAC {
	probe, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: dst, Port: discardPort})
	if err != nil {
		return model.MAC{}
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
func macHolding(addr net.IP, ifs []net.Interface, addrs func(net.Interface) ([]net.Addr, error)) model.MAC {
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
				return model.MAC(ifc.HardwareAddr)
			}
		}
	}
	return model.MAC{}
}

// hostLocator is the lookup a client uses: Locate with the client's own ports, logging to the
// client's logger (before it is identified: Locate does that). It probes the interfaces of this
// host, except for a client whose Config.BroadcastAddr is not the limited broadcast: that client was
// pointed at one address (a fake switch on loopback, or one subnet), and its lookup goes there only,
// never to the real network on the default ports.
func hostLocator(log *slog.Logger, cfg Config) func(ctx context.Context, mac model.MAC) (model.Info, error) {
	env := discoverEnv{interfaces: hostInterfaces, switchPort: cfg.SwitchPort, hostPort: cfg.HostPort}
	if b := cfg.BroadcastAddr; b.IsValid() && b != limitedBroadcast {
		env.interfaces = func() ([]netInterface, error) {
			return []netInterface{{Name: "configured", MAC: localMAC(net.IP(b.AsSlice())), Broadcasts: []netip.Addr{b}}}, nil
		}
	}
	return func(ctx context.Context, mac model.MAC) (model.Info, error) {
		return locateWith(ctx, mac, DiscoverOptions{Logger: log}, env)
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
