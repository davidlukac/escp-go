package escp

import "fmt"

// Impact classes what kind of change a write is, by what it can do to the way the switch is
// reached: a method that can cut the management path or the uplink with the right arguments
// (a port that is turned off or retuned, a mirror destination, a VLAN or LAG membership, an
// address) is ImpactManagement, whichever port it is aimed at. It is a bound for a gate or a front
// end deciding how much confirmation a write needs; it cannot say whether a particular write is
// harmless (a speed change on a free port is not, to the library, different from one on the
// uplink). That needs the ports: see Operation.Ports and the safety package.
type Impact int

// The impact classes, from harmless to able to cut the management path. The zero value is
// not a class: a writer that forgot to declare one would be refused by every gate that
// compares.
const (
	// ImpactCosmetic cannot affect traffic or the way the switch is reached (LED, device
	// description).
	ImpactCosmetic Impact = iota + 1
	// ImpactTraffic can slow or drop some of the traffic of the ports it names or of the whole
	// switch, but takes no port, address or VLAN away: rate limits, storm control, QoS and
	// IGMP snooping.
	ImpactTraffic
	// ImpactManagement can cut the management path or the uplink, or leave the switch
	// unreachable: its IP settings and DHCP, the VLAN mode, port-based and 802.1Q VLAN
	// membership and PVIDs, port state and speed, link aggregation, port mirroring and loop
	// prevention.
	ImpactManagement
)

// Valid reports whether i is one of the three classes.
func (i Impact) Valid() bool { return i >= ImpactCosmetic && i <= ImpactManagement }

// String returns the lower-case name of the class, or unknown(n) for a value that is none.
func (i Impact) String() string {
	switch i {
	case ImpactCosmetic:
		return "cosmetic"
	case ImpactTraffic:
		return "traffic"
	case ImpactManagement:
		return "management"
	}
	return fmt.Sprintf("unknown(%d)", int(i))
}

// Group names the group of ports a write replaces or removes, by the id the switch knows it by.
// At most one field is set; the zero value is no group.
type Group struct {
	// LAG is a link aggregation group (SetLAG).
	LAG LAGID
	// PortVLAN is a port-based VLAN group (SetPortBasedVLAN).
	PortVLAN PortVLANID
	// Dot1QVLAN is an 802.1Q VLAN (SetDot1QVLAN, DeleteDot1QVLAN).
	Dot1QVLAN VLANID
}

// IsZero reports whether no group is named.
func (g Group) IsZero() bool { return g == Group{} }

// Operation describes a write to a WriteGate before it is sent.
type Operation struct {
	// Name is the name of the client method that is writing ("SetPort", "SetLAG").
	Name string
	// Ports are the ports the write names: the mirror's destination and sources, the members
	// of a LAG or a VLAN, the port of a port setting. Empty for a write that names none.
	//
	// These are the ports named, not every port affected: a write that replaces a group
	// (Replaces below) also takes the ports away that were members before and are not
	// named now, and removing a group names none. A gate that has to protect those ports
	// reads the group's members itself (safety.PortGate does).
	Ports PortSet
	// Impact is the class of the method; see Impact. It is a bound for the method, not a
	// verdict on these arguments: it does not know which port is the uplink.
	Impact Impact
	// Replaces is the group the write replaces or removes, when it does: its current members lose
	// their membership unless the write names them again.
	Replaces Group
}
