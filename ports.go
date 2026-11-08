package escp

import (
	"context"
	"fmt"

	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
)

// Layout of a port entry (TLV 4096): one byte per field.
const (
	portEntryLen    = 7
	portOffPort     = 0
	portOffState    = 1
	portOffLAG      = 2
	portOffCfgSpeed = 3
	portOffActSpeed = 4
	portOffCfgFlow  = 5
	portOffActFlow  = 6
)

// speedFromCode names a speed code of a read reply (0 = no link).
func speedFromCode(b byte) model.Speed {
	if b == 0 {
		return model.SpeedLinkDown
	}
	if speeds := model.ConfigurableSpeeds(); int(b) <= len(speeds) {
		return speeds[b-1]
	}
	return model.Speed(fmt.Sprintf("unknown(%d)", b))
}

// speedCode returns the wire code of a configurable speed.
func speedCode(s model.Speed) (byte, error) {
	for i, n := range model.ConfigurableSpeeds() {
		if n == s {
			return byte(i + 1), nil
		}
	}
	return 0, invalidf("escp: unknown speed %q (valid: %v)", string(s), model.ConfigurableSpeeds())
}

// parsePorts decodes TLV 4096 values: port, state, lag, cfg speed, actual
// speed, cfg flow control, actual flow control.
func parsePorts(tlvs []wire.TLV) ([]model.Port, error) {
	var ports []model.Port
	for _, t := range tlvs {
		if t.Type != wire.TLVPorts {
			continue
		}
		v := t.Value
		if len(v) < portEntryLen {
			return nil, fmt.Errorf("escp: port TLV too short (%d bytes)", len(v))
		}
		ports = append(ports, model.Port{
			ID: model.PortID(v[portOffPort]), Enabled: v[portOffState] == 1, LAG: model.LAGID(v[portOffLAG]),
			ConfiguredSpeed: speedFromCode(v[portOffCfgSpeed]), ActualSpeed: speedFromCode(v[portOffActSpeed]),
			ConfiguredFlowControl: v[portOffCfgFlow] == 1, ActualFlowControl: v[portOffActFlow] == 1,
		})
	}
	return ports, nil
}

// Ports reads the Port Setting page: state, LAG membership, speed and flow control per port.
func (c *Client) Ports(ctx context.Context) ([]model.Port, error) {
	tlvs, err := c.getTLVs(ctx, wire.TLVPorts)
	if err != nil {
		return nil, err
	}
	return parsePorts(tlvs)
}

// SetPort writes the configuration of one port: admin state, speed and flow
// control (7-byte TLV 4096 as sent by the vendor utility). Flow control can only be
// on with speed auto or 1000Mbps-full (see model.Speed.AllowsFlowControl): any other
// combination is refused before anything is sent, because the switch would accept
// it and silently keep flow control off. Changing a port that carries traffic can
// cut connectivity; callers must guard against that.
//
// For a port that is a member of a LAG the switch refuses half duplex (the error names
// that), but it accepts a different speed, flow control or admin state and so lets
// the members drift apart: keep them alike, or change the whole group.
func (c *Client) SetPort(ctx context.Context, cfg model.PortConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	code, _ := speedCode(cfg.Speed) // Validate accepted the speed
	if err := c.requirePorts(ctx, cfg.ID.AsSet()); err != nil {
		return err
	}
	entry := make([]byte, portEntryLen)
	entry[portOffPort], entry[portOffState], entry[portOffCfgSpeed], entry[portOffCfgFlow] = byte(cfg.ID), boolByte(cfg.Enabled), code, boolByte(cfg.FlowControl)
	return nameRefusal(c.setTLVs(ctx, model.MethodSetPort.Operation().On(cfg.ID.AsSet()), wire.TLV{Type: wire.TLVPorts, Value: entry}), portRefusals)
}

// portRefusals names the refusals of a port write that were provoked on a real switch
// (firmware 20250710): half duplex on a port that is a member of a LAG. Half duplex on a
// port that is not a member is accepted.
var portRefusals = map[ReplyCode]string{
	lagRefusedHalfDuplex: "the port is a member of a LAG, and LAG members must be full duplex",
}
