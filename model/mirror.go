package model

// Mirror is the port mirroring configuration (Monitoring > Port Mirror): the traffic of the
// source ports is copied to Destination. When Enabled is false the switch reports no ports.
type Mirror struct {
	Enabled      bool
	Destination  PortID  // monitoring (destination) port, 0 when disabled
	IngressPorts PortSet // source ports whose incoming traffic is copied
	EgressPorts  PortSet // source ports whose outgoing traffic is copied
}

// Validate checks the configuration before it is written. An enabled mirror has a valid
// destination, at least one source port in either direction, and does not mirror the destination
// to itself; a disabled one is the zero Mirror (what SetPortMirror takes to switch mirroring
// off). The error matches ErrInvalid.
func (m Mirror) Validate() error {
	if !m.Enabled {
		if m.Destination != 0 || !m.IngressPorts.IsEmpty() || !m.EgressPorts.IsEmpty() {
			return invalidf("escp: a disabled mirror must have no destination or source ports")
		}
		return nil
	}
	if err := m.Destination.Validate(); err != nil {
		return err
	}
	sources := m.IngressPorts.Union(m.EgressPorts)
	if sources.IsEmpty() {
		return invalidf("escp: port mirroring needs at least one ingress or egress source port")
	}
	if sources.Has(m.Destination) {
		return invalidf("escp: port %d cannot mirror itself", m.Destination)
	}
	return nil
}
