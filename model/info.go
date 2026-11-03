package model

import (
	"net/netip"
)

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
