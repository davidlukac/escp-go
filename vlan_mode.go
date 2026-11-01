package escp

import (
	"context"

	"github.com/davidlukac/escp-go/internal/wire"
)

// VLANMode is which of the switch's three VLAN modes is active. They exclude each other: the
// vendor utility warns that switching discards the settings of the other modes. The values
// are the spellings consumers keep in saved state (the Terraform provider does), so they do not change.
type VLANMode string

// The VLAN modes. VLANModeNone is what VLANMode reports for a switch with no mode on, or with
// several on (which the firmware should not allow); it cannot be written.
const (
	VLANModeNone      VLANMode = "none"
	VLANModeMTU       VLANMode = "mtu"
	VLANModePortBased VLANMode = "port-based"
	VLANModeDot1Q     VLANMode = "802.1q"
)

// VLANModes lists the modes SetVLANMode accepts.
func VLANModes() []VLANMode { return []VLANMode{VLANModeMTU, VLANModePortBased, VLANModeDot1Q} }

// ParseVLANMode converts a mode name as the constants spell it; VLANModeNone, which cannot be
// written, and unknown names are errors that match ErrInvalid.
func ParseVLANMode(name string) (VLANMode, error) {
	for _, m := range VLANModes() {
		if string(m) == name {
			return m, nil
		}
	}
	return "", invalidf("escp: unknown VLAN mode %q (valid: %v)", name, VLANModes())
}

// Validate reports whether m is a mode SetVLANMode accepts (ParseVLANMode's set); VLANModeNone is
// only ever reported and is refused. The error matches ErrInvalid.
func (m VLANMode) Validate() error {
	_, err := ParseVLANMode(string(m))
	return err
}

// UnmarshalText reads a mode name, so that a decoder refuses what no switch reports: the three modes
// and VLANModeNone (a reading names it). An error matches ErrInvalid. The text of a VLANMode is the
// value itself, so nothing is needed to encode one.
func (m *VLANMode) UnmarshalText(text []byte) error {
	for _, known := range append(VLANModes(), VLANModeNone) {
		if string(known) == string(text) {
			*m = known
			return nil
		}
	}
	return invalidf("escp: unknown VLAN mode %q (valid: %v or %s)", text, VLANModes(), VLANModeNone)
}

// VLANModeSetting is the active VLAN mode and, for the MTU VLAN, its uplink port (zero for
// every other mode).
type VLANModeSetting struct {
	Mode       VLANMode
	UplinkPort PortID
}

// VLANModeFrom names the active mode from the three readings that carry a mode flag (MTUVLAN,
// PortBasedVLAN and Dot1Q, each with its Enabled). No flag, or more than one, is VLANModeNone: a
// state the client cannot write, shown as it is instead of being hidden.
func VLANModeFrom(mtu MTUVLAN, portBased PortBasedVLAN, dot1q Dot1Q) VLANModeSetting {
	active, st := 0, VLANModeSetting{Mode: VLANModeNone}
	if mtu.Enabled {
		active, st = active+1, VLANModeSetting{Mode: VLANModeMTU, UplinkPort: mtu.UplinkPort}
	}
	if portBased.Enabled {
		active, st = active+1, VLANModeSetting{Mode: VLANModePortBased}
	}
	if dot1q.Enabled {
		active, st = active+1, VLANModeSetting{Mode: VLANModeDot1Q}
	}
	if active != 1 {
		return VLANModeSetting{Mode: VLANModeNone}
	}
	return st
}

// VLANMode reads the three mode flags (three reads) and names the active mode.
func (c *Client) VLANMode(ctx context.Context) (VLANModeSetting, error) {
	mtu, err := c.MTUVLAN(ctx)
	if err != nil {
		return VLANModeSetting{}, err
	}
	pb, err := c.PortBasedVLAN(ctx)
	if err != nil {
		return VLANModeSetting{}, err
	}
	q, err := c.Dot1Q(ctx)
	if err != nil {
		return VLANModeSetting{}, err
	}
	return VLANModeFrom(mtu, pb, q), nil
}

// Validate checks the setting for SetVLANMode: one of VLANModes; for the MTU VLAN a valid
// uplink port, for the others none. The error matches ErrInvalid.
func (s VLANModeSetting) Validate() error {
	switch s.Mode {
	case VLANModeMTU:
		return s.UplinkPort.Validate()
	case VLANModePortBased, VLANModeDot1Q:
		if s.UplinkPort != 0 {
			return invalidf("escp: VLAN mode %s has no uplink port (got %d)", s.Mode, s.UplinkPort)
		}
		return nil
	default:
		return invalidf("escp: VLAN mode %q cannot be set (valid: %v)", s.Mode, VLANModes())
	}
}

// SetVLANMode makes the given VLAN mode the active one; the switch turns the other two off and
// discards their settings, which can cut every connection, including the management path.
// Callers must guard. With the MTU VLAN every port can only talk to the uplink port. Writing
// the mode the switch already has is harmless but not skipped: compare with VLANMode first.
func (c *Client) SetVLANMode(ctx context.Context, s VLANModeSetting) error {
	if err := s.Validate(); err != nil {
		return err
	}
	var tlv wire.TLV
	switch s.Mode {
	case VLANModeMTU:
		if err := c.requirePorts(ctx, s.UplinkPort.set()); err != nil {
			return err
		}
		tlv = wire.TLV{Type: wire.TLVVLANMTU, Value: []byte{boolByte(true), byte(s.UplinkPort)}}
	case VLANModePortBased:
		tlv = wire.TLV{Type: wire.TLVVLANPortBased, Value: []byte{boolByte(true)}}
	default: // VLANModeDot1Q: Validate refused every other mode
		tlv = wire.TLV{Type: wire.TLVVLAN8021Q, Value: []byte{boolByte(true)}}
	}
	op := Operation{Name: "SetVLANMode", Impact: ImpactManagement}
	if s.Mode == VLANModeMTU {
		op.Ports = s.UplinkPort.set()
	}
	return c.setTLVs(ctx, op, tlv)
}
