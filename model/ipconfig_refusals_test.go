package model

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
)

// The refusals of IPConfig.Validate, one cause at a time. A refusal that comes from another rule of
// the validator still matches ErrInvalid, so TestIPConfigValidatePrefixBounds cannot tell which rule
// fired; these cases name the rule by a fragment of its message, which is how a rule that is
// weakened to "refused for some other reason" is noticed (the && and | operator mutants of the
// extended mutation run, #195).

func TestIPConfigValidateNamesTheRuleThatRefuses(t *testing.T) {
	addr := netip.MustParseAddr
	const (
		notIPv4  = "must all be IPv4 addresses"
		badMask  = "is not a contiguous prefix between /8 and /30"
		badHost  = "is not a usable host address in 10.1.2.0/24"
		badGW    = "must be another host address in 10.1.2.0/24"
		mask24   = "255.255.255.0"
		hostAddr = "10.1.2.3"
		gwAddr   = "10.1.2.4"
	)
	cfg := func(ip, mask, gw netip.Addr) IPConfig { return IPConfig{IP: ip, Netmask: mask, Gateway: gw} }
	tests := []struct {
		name    string
		cfg     IPConfig
		wantErr string // a fragment of the message; empty means the configuration is valid
	}{
		{"valid", cfg(addr(hostAddr), addr(mask24), addr(gwAddr)), ""},
		{"valid without a gateway", cfg(addr(hostAddr), addr(mask24), addr("0.0.0.0")), ""},

		// Line 42: each of the three addresses alone is not IPv4, the other two are fine.
		{"only the address is IPv6", cfg(addr("2001:db8::1"), addr(mask24), addr(gwAddr)), notIPv4},
		{"only the address is the zero value", cfg(netip.Addr{}, addr(mask24), addr(gwAddr)), notIPv4},
		{"only the mask is IPv6", cfg(addr(hostAddr), addr("ffff:ffff::"), addr(gwAddr)), notIPv4},
		{"only the mask is the zero value", cfg(addr(hostAddr), netip.Addr{}, addr(gwAddr)), notIPv4},
		{"only the gateway is IPv6", cfg(addr(hostAddr), addr(mask24), addr("2001:db8::1")), notIPv4},
		{"only the gateway is the zero value", cfg(addr(hostAddr), addr(mask24), netip.Addr{}), notIPv4},
		{"an IPv4-mapped IPv6 address is not IPv4", cfg(addr("::ffff:10.1.2.3"), addr(mask24), addr(gwAddr)), notIPv4},

		{"a mask that is not a prefix", cfg(addr(hostAddr), addr("255.0.255.0"), addr(gwAddr)), badMask},

		// The network address and the broadcast address are not usable hosts (isHostAddress and broadcastOf).
		{"the address is the network address", cfg(addr("10.1.2.0"), addr(mask24), addr(gwAddr)), badHost},
		{"the address is the broadcast address", cfg(addr("10.1.2.255"), addr(mask24), addr(gwAddr)), badHost},
		{"the first host", cfg(addr("10.1.2.1"), addr(mask24), addr(gwAddr)), ""},
		{"the last host", cfg(addr("10.1.2.254"), addr(mask24), addr(gwAddr)), ""},

		// Line 57: each cause of a refused gateway alone.
		{"a gateway outside the subnet", cfg(addr(hostAddr), addr(mask24), addr("10.1.3.4")), badGW},
		{"a gateway that is the address itself", cfg(addr(hostAddr), addr(mask24), addr(hostAddr)), badGW},
		{"a gateway that is the network address", cfg(addr(hostAddr), addr(mask24), addr("10.1.2.0")), badGW},
		{"a gateway that is the broadcast address", cfg(addr(hostAddr), addr(mask24), addr("10.1.2.255")), badGW},
		{"a gateway that is the first host", cfg(addr(hostAddr), addr(mask24), addr("10.1.2.1")), ""},
		{"a gateway that is the last host", cfg(addr(hostAddr), addr(mask24), addr("10.1.2.254")), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("Validate() = %v, want an error that matches ErrInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("Validate() = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}
