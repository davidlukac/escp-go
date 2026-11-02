package escp

import (
	"context"
	"log/slog"
	"net/netip"
	"strings"
	"time"
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

// Config configures a Client. Zero values select sensible defaults, except Host, which is
// required. A Config holds the switch's password as a Password, which prints as [redacted], so a
// Config can be logged or put in an error message; keep the text itself (Reveal) out of both.
type Config struct {
	// Host is the switch's IPv4 address or hostname (no port: that is SwitchPort; no IPv6). It
	// is required unless MAC is set, in which case the address is found by discovery on the
	// first request. An address that reaches no single device (0.0.0.0, a multicast group,
	// 255.255.255.255) is refused.
	Host string
	// MAC, when set, pins the identity of the switch: a device that answers with another MAC
	// address is never written to (the request fails with ErrWrongSwitch). With no Host, or when
	// Host does not answer, the switch is looked up by MAC with a discovery probe on the first
	// request and again when it stops answering (its address may have changed); nothing is sent
	// by Dial.
	MAC MAC
	// Username and Password are sent with every write and every authenticated read.
	Username string
	Password Password
	Timeout  time.Duration // how long to wait for a reply per attempt, default 3s
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
	// default 255.255.255.255.
	BroadcastAddr netip.Addr

	// WriteGate, when set, is consulted before every write; see WriteGate.
	WriteGate WriteGate

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
// more than one device), no negative Timeout, no more than MaxRetries Retries, ports that are UDP ports and an IPv4 BroadcastAddr.
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
	if a, err := netip.ParseAddr(host); err == nil && (a.IsUnspecified() || a.IsMulticast() || a == limitedBroadcast) {
		return invalidf("escp: Host %s does not name one switch (an unspecified, multicast or broadcast address)", host)
	}
	return nil
}

// Reader is the part of a Client that a WriteGate may use: the reads of the settings, so that a gate
// can tell what a write would disturb (which ports have a link, which belong to a group, which firmware
// runs). It only reads. A *Client is a Reader; a test double implements the few methods its gate calls
// and embeds the interface for the rest.
type Reader interface {
	Info(ctx context.Context) (Info, error)
	System(ctx context.Context) (System, error)
	LED(ctx context.Context) (bool, error)
	LoopPrevention(ctx context.Context) (bool, error)
	Ports(ctx context.Context) ([]Port, error)
	LAGs(ctx context.Context) ([]LAG, error)
	PortMirror(ctx context.Context) (Mirror, error)
	Bandwidth(ctx context.Context) ([]PortBandwidth, error)
	StormControl(ctx context.Context) ([]PortStorm, error)
	QoS(ctx context.Context) (QoS, error)
	IGMP(ctx context.Context) (IGMP, error)
	VLANMode(ctx context.Context) (VLANModeSetting, error)
	MTUVLAN(ctx context.Context) (MTUVLAN, error)
	PortBasedVLAN(ctx context.Context) (PortBasedVLAN, error)
	Dot1Q(ctx context.Context) (Dot1Q, error)
}

// Compile-time proof that a Client can be handed to a gate.
var _ Reader = (*Client)(nil)

// WriteGate decides whether the client may send a write to the switch. Every
// method that changes the switch (each Set* and Delete*) calls it first, with
// the client itself and a description of the write (its method, the ports it
// names and its Impact), and sends nothing when it returns an error: the
// method returns that error unchanged. A gate may read from the switch through
// sw but must never write, and must be safe for concurrent use. Reads are
// never gated.
//
// A method that sends several writes consults the gate before each of them
// with the same operation, so a gate that is expensive should remember its
// verdict.
type WriteGate func(ctx context.Context, sw Reader, op Operation) error

// Info is what a discovery reply reports about the switch.
type Info struct {
	Model, Hardware string
	// Hostname is the device description.
	Hostname Hostname
	// Firmware is the version the switch reports, read into its parts.
	Firmware Firmware
	// MAC is the switch's hardware address; the zero value when the reply carried none.
	MAC MAC
	// IP, Netmask and Gateway are the management address the switch reports. An address the reply
	// did not carry is the invalid netip.Addr (Addr.IsValid is false), which is not the same as
	// 0.0.0.0: a gateway of 0.0.0.0 means "no gateway".
	IP, Netmask, Gateway netip.Addr
	DHCP                 bool
	// FactoryDefault is discovery TLV 14: the switch has never been configured
	// (still on factory credentials).
	FactoryDefault bool
}
