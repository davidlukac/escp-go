package escp

import (
	"context"
	"errors"
	"fmt"

	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
)

// Layout of a LAG entry (TLV 4608): group id (1 byte), member port mask (maskLen bytes).
const (
	lagOffMask  = 1
	lagEntryLen = lagOffMask + maskLen
)

// Reply codes of a refused LAG write, found by provoking each conflict on a
// real TL-SG108E (firmware 20250710) and reading the code. The member rules
// (speed, duplex, negotiation, flow control, enabled state, QoS priority) are the
// ones the vendor utility checks before it sends the write; the switch enforces
// them itself when the LAG is created.
const (
	lagRefusedPortRange    ReplyCode = 4    // ports outside the group's range
	lagRefusedPortVLAN     ReplyCode = 7401 // a port is in a port-based VLAN other than the default
	lagRefusedBandwidth    ReplyCode = 7403 // a port has a bandwidth limit
	lagRefusedStorm        ReplyCode = 7404 // a port has storm control
	lagRefusedQoS          ReplyCode = 7405 // member ports have different QoS priorities (checked in port-based QoS mode only)
	lagRefusedSpeed        ReplyCode = 7406 // member ports have different fixed speeds
	lagRefusedHalfDuplex   ReplyCode = 7407 // a member port is set to half duplex (also refused when a port write would make a member half duplex)
	lagRefusedNegotiation  ReplyCode = 7410 // member ports mix auto-negotiation with a fixed speed
	lagRefusedSinglePort   ReplyCode = 7415 // fewer than two ports
	lagRefusedFlowControl  ReplyCode = 7417 // member ports have different flow control settings
	lagRefusedEnabled      ReplyCode = 7418 // member ports are not all enabled or all disabled
	lagRefusedMirrorTarget ReplyCode = 7205 // a port is the mirror destination
	lagRefusedMirrorSource ReplyCode = 7206 // a port is mirrored
)

var lagRefusals = map[ReplyCode]string{
	lagRefusedPortRange:    "the ports do not fit the group: LAG 1 takes ports 1-4 and LAG 2 takes ports 5-8",
	lagRefusedPortVLAN:     "a member port belongs to a port-based VLAN other than the default one",
	lagRefusedBandwidth:    "a member port has a bandwidth limit",
	lagRefusedStorm:        "a member port has storm control enabled",
	lagRefusedQoS:          "the member ports have different QoS priorities",
	lagRefusedSpeed:        "the member ports do not have the same speed",
	lagRefusedHalfDuplex:   "a member port is set to half duplex; LAG members must be full duplex",
	lagRefusedNegotiation:  "the member ports mix auto-negotiation with a fixed speed",
	lagRefusedMirrorTarget: "a member port is the destination of the port mirror",
	lagRefusedMirrorSource: "a member port is mirrored (a source of the port mirror)",
	lagRefusedSinglePort:   "a LAG needs at least two ports",
	lagRefusedFlowControl:  "the member ports do not have the same flow control setting",
	lagRefusedEnabled:      "the member ports are not all enabled or all disabled",
}

// parseLAGs decodes TLV 4608: group id (1 byte), member port bitmap (4 bytes).
func parseLAGs(tlvs []wire.TLV) ([]model.LAG, error) {
	var out []model.LAG
	for _, t := range tlvs {
		if t.Type != wire.TLVLAG {
			continue
		}
		if len(t.Value) < lagEntryLen {
			return nil, fmt.Errorf("escp: LAG TLV too short (%d bytes)", len(t.Value))
		}
		out = append(out, model.LAG{ID: model.LAGID(t.Value[0]), Ports: maskAt(t.Value, lagOffMask)})
	}
	return out, nil
}

// LAGs reads the link aggregation groups (Switching > Port Trunk), unused ones included with no ports.
func (c *Client) LAGs(ctx context.Context) ([]model.LAG, error) {
	tlvs, err := c.getTLVs(ctx, wire.TLVLAG)
	if err != nil {
		return nil, err
	}
	return parseLAGs(tlvs)
}

// SetLAG creates or replaces a link aggregation group: TLV 4608 with the group
// id and the member port mask. An empty port set removes the group. Group
// and ports are checked against model.LAG.Validate first. The switch itself refuses
// ports that conflict with mirroring, a port-based VLAN, a bandwidth limit or
// storm control, and member ports that differ in speed, negotiation, flow control,
// enabled state or (in port-based QoS mode) QoS priority, or that are half duplex;
// such a refusal comes back as a *RejectedError that names the conflict. The switch
// checks these only when the group is written: it does not keep members alike
// afterwards (see SetPort). Changing a group whose ports carry traffic can cut the
// uplink; callers must guard against that.
func (c *Client) SetLAG(ctx context.Context, lag model.LAG) error {
	if err := lag.Validate(); err != nil {
		return err
	}
	if err := c.requirePorts(ctx, lag.Ports); err != nil {
		return err
	}
	return nameRefusal(c.setTLVs(ctx, model.MethodSetLAG.Operation().On(lag.Ports).Replacing(model.Group{LAG: lag.ID}), wire.TLV{Type: wire.TLVLAG, Value: append([]byte{byte(lag.ID)}, lag.Ports.Mask()...)}), lagRefusals)
}

// nameRefusal adds the reason to a *RejectedError whose code is in reasons; any other
// error, and a refusal with an unknown code, is returned as it is.
func nameRefusal(err error, reasons map[ReplyCode]string) error {
	if rejected, ok := errors.AsType[*RejectedError](err); ok {
		if reason, ok := reasons[rejected.Code]; ok {
			return &RejectedError{Code: rejected.Code, Reason: reason}
		}
	}
	return err
}
