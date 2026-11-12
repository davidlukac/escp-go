package model

import (
	"regexp"
	"strconv"
)

// Limits of 802.1Q VLAN ids and names. The id range is the IEEE one; the name rules are those of
// the vendor utility (at most ten characters from letters, digits, hyphen and underscore).
const (
	MinVLANID      VLANID = 1
	MaxVLANID      VLANID = 4094
	MaxVLANNameLen        = 10
)

var vlanNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]*$`)

// VLANID is the id of an 802.1Q VLAN, and the PVID of a port, which is the id of the VLAN its
// untagged traffic enters. The IEEE range is MinVLANID to MaxVLANID; two bytes carry it on the wire.
// (Port-based VLAN groups and LAG groups have ids of their own, PortVLANID and LAGID, one byte each.)
type VLANID int

// Validate reports whether v is within MinVLANID to MaxVLANID; the error matches ErrInvalid.
func (v VLANID) Validate() error {
	if v < MinVLANID || v > MaxVLANID {
		return invalidf("escp: invalid VLAN id %d (valid: %d-%d)", v, MinVLANID, MaxVLANID)
	}
	return nil
}

// String is the id as a number.
func (v VLANID) String() string { return strconv.Itoa(int(v)) }

// VLANName is the name of an 802.1Q VLAN: empty, or up to MaxVLANNameLen letters, digits, hyphens and
// underscores (the vendor utility's rules).
type VLANName string

// Validate applies the rules; the error matches ErrInvalid.
func (n VLANName) Validate() error {
	if len(n) > MaxVLANNameLen || !vlanNamePattern.MatchString(string(n)) {
		return invalidf("escp: invalid VLAN name %s (at most %d letters, digits, hyphens or underscores)", quoteBounded(string(n)), MaxVLANNameLen)
	}
	return nil
}

// UnmarshalText reads a VLAN name with the rule of Validate, so that a decoder (encoding/json for a
// string, a map key, a configuration file) refuses what a writer refuses; the error is Validate's and
// matches ErrInvalid, and the receiver is left alone on a refusal. The empty text is a name, as it
// is for Validate.
func (n *VLANName) UnmarshalText(text []byte) error {
	v := VLANName(text)
	if err := v.Validate(); err != nil {
		return err
	}
	*n = v
	return nil
}

// String is the name.
func (n VLANName) String() string { return string(n) }

// Dot1QVLAN is one 802.1Q VLAN with its tagged and untagged members.
type Dot1QVLAN struct {
	ID       VLANID
	Name     VLANName
	Tagged   PortSet
	Untagged PortSet
}

// PortPVID is the default VLAN of a port.
type PortPVID struct {
	Port PortID
	PVID VLANID
}

// Dot1Q is VLAN > 802.1Q VLAN plus PVID settings.
type Dot1Q struct {
	Enabled  bool
	MaxVLANs int
	VLANs    []Dot1QVLAN
	PVIDs    []PortPVID
}

// CheckRoom reports whether the table can take VLAN id: it holds it already (changing a VLAN needs no
// room), or it has fewer entries than the most the switch supports. MaxVLANs counts VLAN 1: a
// TL-SG108E on firmware 20250710, which reports 32, took 31 more VLANs beside VLAN 1 and refused the
// next with code 7505.
// A switch that did not report its maximum is not bounded here. The error matches ErrInvalid.
func (q Dot1Q) CheckRoom(id VLANID) error {
	if q.MaxVLANs == 0 {
		return nil
	}
	for _, v := range q.VLANs {
		if v.ID == id {
			return nil
		}
	}
	if len(q.VLANs) >= q.MaxVLANs {
		return invalidf("escp: the 802.1Q VLAN table is full: the switch holds at most %d VLANs (VLAN 1 among them) and has %d, so VLAN %d cannot be added", q.MaxVLANs, len(q.VLANs), id)
	}
	return nil
}
