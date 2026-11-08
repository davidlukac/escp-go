package escp

import (
	"log/slog"
	"net/netip"
	"strings"
	"time"

	"github.com/davidlukac/escp-go/model"
)

// Default ports of the protocol: requests go to 29808, replies always come
// back to 29809 regardless of the source port.
const (
	defaultSwitchPort = 29808
	defaultHostPort   = 29809
)

// NoRetries is the Config.Retries value that asks for a single attempt per request. (Zero
// cannot mean that: it selects the default.)
const NoRetries = -1

// defaultRetries is the number of additional attempts when Config.Retries is zero.
const defaultRetries = 2

// MaxRetries is the highest Config.Retries that Config.Validate and Dial accept. A switch that
// stays silent holds a call for (Retries+1) x Timeout per request, so an unbounded value is a call
// that does not return for days; ten additional attempts is already more than a switch that does
// not answer the first three will ever answer.
const MaxRetries = 10

// MaxTimeout is the highest Config.Timeout that Config.Validate and Dial accept: the wait for a reply
// per attempt. A switch answers in well under three seconds (it stalled for about ten under heavy
// load); with MaxRetries the longest a request can hold a call is (MaxRetries+1) x MaxTimeout, 330
// seconds, and a Timeout of an hour would hold it for hours.
const MaxTimeout = 30 * time.Second

// Config configures a Client. Zero values select sensible defaults, except Host, which is
// required. A Config holds the switch's password as a Password, which prints as [redacted], so a
// Config can be logged or put in an error message; keep the text itself (Reveal) out of both.
type Config struct {
	// Host is the switch's IPv4 address or hostname (no port: that is SwitchPort; no IPv6). It
	// is required unless MAC is set, in which case the address is found by discovery on the
	// first request. An address that reaches no single device (0.0.0.0, a multicast group,
	// 255.255.255.255) is refused, whether it is written out or a name resolves to it.
	Host string
	// MAC, when set, pins the identity of the switch: a device that answers with another MAC
	// address is never written to (the request fails with ErrWrongSwitch). With no Host the switch
	// is looked up by MAC with a discovery probe on the first request and again when it stops
	// answering (its address may have changed); with a Host the client keeps to that address
	// unless RelocateByMAC is set. Nothing is sent by Dial. Without it the client pins itself to the MAC the switch reports on its first
	// contact, so a device that takes the address over later is refused the same way; only the
	// first contact is then taken on trust.
	MAC model.MAC

	// RelocateByMAC lets a client that has both Host and MAC look its switch up by the MAC when
	// the switch does not answer at Host, and follow it to the address found. It is off by
	// default: the answer to a lookup is not authenticated, so a client that was given an address
	// keeps to it. A client with a MAC and no Host always looks the switch up (it has no address
	// to keep to), and SetIPConfig and EnableDHCP follow their own switch whatever this says.
	// Turn it on for a switch whose address is not stable (a DHCP lease the configuration still
	// names).
	RelocateByMAC bool
	// Username and Password are sent with every write and every authenticated read.
	Username string
	Password model.Password
	Timeout  time.Duration // how long to wait for a reply per attempt, default 3s, at most MaxTimeout
	// Retries is the number of additional attempts after the first when the switch stays
	// silent: zero selects the default (2), NoRetries (any negative value) selects none, and
	// more than MaxRetries is refused.
	// One request is therefore sent up to Retries+1 times, each waiting up to Timeout. A call
	// that needs a session makes up to four such requests (the key exchange and the request,
	// then both again if the switch stops answering mid-call), plus the discovery that learns
	// the switch's MAC on a client's first call: with the defaults 36 seconds, 45 on the first.
	Retries int

	// SwitchPort and HostPort are the UDP ports of the switch (29808) and of this host
	// (29809). A real switch always replies to 29809 whatever port the request came from,
	// so change them only to talk to a fake switch.
	SwitchPort int
	HostPort   int
	// BroadcastAddr is where discovery probes go besides the switch's own address;
	// default 255.255.255.255. The lookup of a client that finds its switch by MAC probes the
	// interfaces of this host, unless BroadcastAddr is another address: then it probes that one
	// only, on SwitchPort and HostPort.
	BroadcastAddr netip.Addr

	// WriteGate, when set, is consulted before every write; see WriteGate.
	WriteGate model.WriteGate

	// Logger receives the client's messages: requests and replies (types and sizes, never
	// values) at Debug, sessions, lookups and writes at Info, retries and repeated downloads
	// at Warn. Nil discards them. The library writes nowhere else, and no message carries the
	// password, the username or the session key.
	Logger *slog.Logger
}

// maxUDPPort is the highest UDP port number.
const maxUDPPort = 1<<16 - 1

// Validate checks the configuration the way Dial does, before anything is resolved or sent: a switch
// to talk to (Host or MAC; a Host that is no IPv6 address, no host:port and no address that reaches
// more than one device), no negative Timeout and none above MaxTimeout, no more than MaxRetries Retries, ports that are UDP ports and an IPv4 BroadcastAddr.
// The zero value of every optional field is valid (it selects the default). The error matches
// ErrInvalid.
func (cfg Config) Validate() error {
	if cfg.Host != "" {
		if err := validateHost(cfg.Host); err != nil {
			return err
		}
	}
	switch {
	case cfg.Host == "" && cfg.MAC.IsZero():
		return invalidf("escp: Config needs a Host or a MAC to find the switch by")
	case cfg.Timeout < 0:
		return invalidf("escp: negative Timeout %v", cfg.Timeout)
	case cfg.Timeout > MaxTimeout:
		return invalidf("escp: Timeout %v is above the limit of %v", cfg.Timeout, MaxTimeout)
	case cfg.Retries > MaxRetries:
		return invalidf("escp: Retries %d is above the limit of %d", cfg.Retries, MaxRetries)
	case cfg.SwitchPort < 0 || cfg.SwitchPort > maxUDPPort:
		return invalidf("escp: SwitchPort %d is not a UDP port (1 to %d, or 0 for %d)", cfg.SwitchPort, maxUDPPort, defaultSwitchPort)
	case cfg.HostPort < 0 || cfg.HostPort > maxUDPPort:
		return invalidf("escp: HostPort %d is not a UDP port (1 to %d, or 0 for %d)", cfg.HostPort, maxUDPPort, defaultHostPort)
	case cfg.BroadcastAddr.IsValid() && !cfg.BroadcastAddr.Is4():
		return invalidf("escp: BroadcastAddr %s is not an IPv4 address", cfg.BroadcastAddr)
	}
	return nil
}

// limitedBroadcast is the IPv4 address that every host of the segment answers to.
var limitedBroadcast = netip.AddrFrom4([4]byte{255, 255, 255, 255})

// validateHost refuses a Host that cannot name one switch: it has a colon (an IPv6 address or a
// port; the client speaks IPv4 and takes the port from SwitchPort) or a space, or it is an IPv4
// address that reaches many devices or none (the unspecified address, a multicast group, the
// limited broadcast). A write to such an address would carry the password to every device that
// listens. A name is not looked up here; Dial does that.
func validateHost(host string) error {
	if strings.ContainsAny(host, ": \t") {
		return invalidf("escp: Host %q is not an IPv4 address or a host name (no IPv6, no port, no spaces)", host)
	}
	if a, err := netip.ParseAddr(host); err == nil && namesMoreThanOneDevice(a) {
		return invalidf("escp: Host %s does not name one switch (an unspecified, multicast or broadcast address)", host)
	}
	return nil
}

// namesMoreThanOneDevice reports whether a is an address that reaches no single device: the
// unspecified address, a multicast group or the limited broadcast. A request sent there carries the
// password to every device that listens.
func namesMoreThanOneDevice(a netip.Addr) bool {
	return a.IsUnspecified() || a.IsMulticast() || a == limitedBroadcast
}

// Compile-time proof that a Client can be handed to a gate.
var _ model.Reader = (*Client)(nil)
