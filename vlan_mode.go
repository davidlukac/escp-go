package escp

import (
	"context"

	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
)

// VLANMode reads the three mode flags (three reads) and names the active mode.
func (c *Client) VLANMode(ctx context.Context) (model.VLANModeSetting, error) {
	mtu, err := c.MTUVLAN(ctx)
	if err != nil {
		return model.VLANModeSetting{}, err
	}
	pb, err := c.PortBasedVLAN(ctx)
	if err != nil {
		return model.VLANModeSetting{}, err
	}
	q, err := c.Dot1Q(ctx)
	if err != nil {
		return model.VLANModeSetting{}, err
	}
	return model.VLANModeFrom(mtu, pb, q), nil
}

// SetVLANMode makes the given VLAN mode the active one; the switch turns the other two off and
// discards their settings, which can cut every connection, including the management path.
// Callers must guard: the gate is told the write touches every port (Operation.AllPorts). With the MTU VLAN every port can only talk to the uplink port. Writing
// the mode the switch already has is harmless but not skipped: compare with VLANMode first.
func (c *Client) SetVLANMode(ctx context.Context, s model.VLANModeSetting) error {
	if err := s.Validate(); err != nil {
		return err
	}
	var tlv wire.TLV
	switch s.Mode {
	case model.VLANModeMTU:
		if err := c.requirePorts(ctx, s.UplinkPort.AsSet()); err != nil {
			return err
		}
		tlv = wire.TLV{Type: wire.TLVVLANMTU, Value: []byte{boolByte(true), byte(s.UplinkPort)}}
	case model.VLANModePortBased:
		tlv = wire.TLV{Type: wire.TLVVLANPortBased, Value: []byte{boolByte(true)}}
	default: // VLANModeDot1Q: Validate refused every other mode
		tlv = wire.TLV{Type: wire.TLVVLAN8021Q, Value: []byte{boolByte(true)}}
	}
	op := model.MethodSetVLANMode.Operation()
	op.AllPorts = true // the new mode applies to every port, and the switch discards the old mode's settings
	if s.Mode == model.VLANModeMTU {
		op.Ports = s.UplinkPort.AsSet()
	}
	return c.setTLVs(ctx, op, tlv)
}
