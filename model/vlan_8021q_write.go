package model

// Validate checks a VLAN before it is written: the id, the name, no port both tagged and untagged,
// and at least one member (use DeleteDot1QVLAN to remove a VLAN). The ports are valid by
// construction; whether the switch has them is checked when the VLAN is written. The error matches
// ErrInvalid.
func (v Dot1QVLAN) Validate() error {
	if err := v.ID.Validate(); err != nil {
		return err
	}
	if err := v.Name.Validate(); err != nil {
		return err
	}
	if both := v.Tagged.Intersect(v.Untagged); !both.IsEmpty() {
		return invalidf("escp: port %d is both tagged and untagged in VLAN %d", both.Ports()[0], v.ID)
	}
	if v.Tagged.Union(v.Untagged).IsEmpty() {
		return invalidf("escp: VLAN %d has no member ports; delete it instead", v.ID)
	}
	return nil
}

// Validate checks the port and the VLAN id. The error matches ErrInvalid.
func (p PortPVID) Validate() error {
	if err := p.Port.Validate(); err != nil {
		return err
	}
	return p.PVID.Validate()
}
