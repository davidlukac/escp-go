package model

import (
	"fmt"
	"net"
	"net/netip"
)

// IPConfig is the static management address of the switch (System > IP Setting): three IPv4
// addresses. It is a comparable value; two configurations are the same when they are ==.
type IPConfig struct {
	IP, Netmask, Gateway netip.Addr
}

// ParseIPv4 reads a dotted-quad IPv4 address: no IPv6, no zone, no port. The error matches
// ErrInvalid. It is the one parser of an address a person types; replies are read from the wire.
func ParseIPv4(s string) (netip.Addr, error) {
	a, err := netip.ParseAddr(s)
	if err != nil || !a.Is4() {
		return netip.Addr{}, invalidf("escp: %q is not a dotted IPv4 address", s)
	}
	return a, nil
}

// String formats the triple for messages.
func (cfg IPConfig) String() string {
	return fmt.Sprintf("ip %s mask %s gateway %s", cfg.IP, cfg.Netmask, cfg.Gateway)
}

// Validate checks a static management address before anything is sent:
// all three are IPv4, the mask is a contiguous prefix between /8 and /30, the
// address is a usable host address of its subnet and the gateway is another
// host of the same subnet (0.0.0.0 means no gateway).
func (cfg IPConfig) Validate() error { return validateIPConfig(cfg, false) }

// ValidateAllowingLoopback is Validate for a switch that lives on this host, which a fake switch
// does: it also accepts an address in 127.0.0.0/8. A client uses it only when a test turned that on.
func (cfg IPConfig) ValidateAllowingLoopback() error { return validateIPConfig(cfg, true) }

// validateIPConfig is IPConfig.Validate; allowLoopback lets tests move a fake switch that lives on 127.0.0.0/8.
func validateIPConfig(cfg IPConfig, allowLoopback bool) error {
	if !cfg.IP.Is4() || !cfg.Netmask.Is4() || !cfg.Gateway.Is4() {
		return invalidf("escp: IP address, subnet mask and gateway must all be IPv4 addresses (%s)", cfg)
	}
	ip, mask, gw := net.IP(cfg.IP.AsSlice()), net.IP(cfg.Netmask.AsSlice()), net.IP(cfg.Gateway.AsSlice())
	m := net.IPMask(mask)
	if ones, bits := m.Size(); bits == 0 || ones < 8 || ones > 30 {
		return invalidf("escp: subnet mask %s is not a contiguous prefix between /8 and /30", mask)
	}
	subnet := net.IPNet{IP: ip.Mask(m), Mask: m}
	if !isUsableHost(ip, subnet, allowLoopback) {
		return invalidf("escp: %s is not a usable host address in %s", ip, subnet.String())
	}
	if gw.IsUnspecified() {
		return nil // no gateway
	}
	if !subnet.Contains(gw) || !isHostAddress(gw, subnet) || gw.Equal(ip) {
		return invalidf("escp: gateway %s must be another host address in %s", gw, subnet.String())
	}
	return nil
}

// isUsableHost reports whether ip can be a switch's own address in subnet: a host address (neither the
// network nor the broadcast address), not unspecified, not multicast, not reserved (0.0.0.0/8 "this
// network", 240.0.0.0/4 reserved, which holds the limited broadcast) and not link-local (169.254.0.0/16),
// and not loopback unless allowed. A switch moved to such an address cannot be reached by any host
// that follows the usual rules, and nothing but a reset brings it back.
func isUsableHost(ip net.IP, subnet net.IPNet, allowLoopback bool) bool {
	if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || (ip.IsLoopback() && !allowLoopback) {
		return false
	}
	if v4 := ip.To4(); v4 != nil && (v4[0] == 0 || v4[0] >= 240) {
		return false
	}
	return isHostAddress(ip, subnet)
}

// isHostAddress reports whether ip is neither the network address nor the broadcast address of subnet.
func isHostAddress(ip net.IP, subnet net.IPNet) bool {
	return !ip.Equal(subnet.IP) && !ip.Equal(broadcastOf(subnet))
}

func broadcastOf(n net.IPNet) net.IP {
	out := make(net.IP, len(n.IP))
	for i := range n.IP {
		out[i] = n.IP[i] | ^n.Mask[i]
	}
	return out
}
