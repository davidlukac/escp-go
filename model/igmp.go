package model

import (
	"net/netip"
)

// IGMPGroup is one multicast group the switch has learned through IGMP snooping.
type IGMPGroup struct {
	IP    netip.Addr // the group address (IPv4 multicast)
	VLAN  VLANID     // the VLAN the group was learned in
	Ports PortSet    // the ports that asked to receive it
}

// IGMP is the Switching > IGMP Snooping page.
type IGMP struct {
	Enabled           bool
	ReportSuppression bool
	Groups            []IGMPGroup
}

// IGMPConfig is what SetIGMP writes. It is separate from IGMP (a reading) because the
// learned groups cannot be set.
type IGMPConfig struct {
	Enabled           bool
	ReportSuppression bool
}
