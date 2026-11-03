package model

// MTUVLAN is VLAN > MTU VLAN: when enabled, every port is isolated from the
// others and only talks to the uplink port.
type MTUVLAN struct {
	Enabled    bool
	UplinkPort PortID
}
