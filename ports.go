package escp

import (
	"context"
	"fmt"

	"github.com/davidlukac/escp-go/internal/wire"
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

// Port is one row of the Port Setting page.
type Port struct {
	ID                    PortID
	Enabled               bool
	LAG                   LAGID // 0 = not in a LAG
	ConfiguredSpeed       Speed // auto, 10Mbps-half, ... 1000Mbps-full
	ActualSpeed           Speed // SpeedLinkDown or a negotiated speed
	ConfiguredFlowControl bool
	ActualFlowControl     bool
}

// Speed is a port's speed and duplex, spelled as the web UI spells it. Consumers keep the
// values in saved state (the Terraform provider does): do not change them.
type Speed string

// The speeds a port can be configured to (codes 1..6 on the wire), and the one state
// that is only ever reported.
const (
	SpeedAuto     Speed = "auto"
	Speed10Half   Speed = "10Mbps-half"
	Speed10Full   Speed = "10Mbps-full"
	Speed100Half  Speed = "100Mbps-half"
	Speed100Full  Speed = "100Mbps-full"
	Speed1000Full Speed = "1000Mbps-full"
	// SpeedLinkDown is the ActualSpeed of a port with no link; it cannot be configured.
	SpeedLinkDown Speed = "link-down"
)

// configurableSpeeds are the speeds SetPort accepts, in the order of the switch's codes 1..6.
var configurableSpeeds = [...]Speed{SpeedAuto, Speed10Half, Speed10Full, Speed100Half, Speed100Full, Speed1000Full}

// ConfigurableSpeeds lists the speeds SetPort accepts, in the order of the switch's codes 1..6.
func ConfigurableSpeeds() []Speed { return append([]Speed(nil), configurableSpeeds[:]...) }

// String returns the speed as spelled in the web UI.
func (s Speed) String() string { return string(s) }

// AllowsFlowControl reports whether flow control can be switched on at this speed. The switch
// keeps it only with auto-negotiation or at 1000Mbps-full; at a fixed 10 or 100 Mbps speed it
// accepts the write but stores flow control as off (measured on firmware 20250710, every
// fixed speed below 1000 Mbps). The vendor utility enforces the same rule: its flow control
// choice is forced to off and disabled unless the speed is auto or 1000 Mbps full, with the
// message "Failed to enable flow control when speed in Force mode".
func (s Speed) AllowsFlowControl() bool { return s == SpeedAuto || s == Speed1000Full }

// ParseSpeed converts a configurable speed name; SpeedLinkDown and unknown names are errors.
func ParseSpeed(name string) (Speed, error) {
	for _, s := range configurableSpeeds {
		if string(s) == name {
			return s, nil
		}
	}
	return "", invalidf("escp: unknown speed %q (valid: %v)", name, configurableSpeeds)
}

// Validate reports whether s is a speed a port can be configured to (ParseSpeed's set);
// SpeedLinkDown is only ever reported and is refused. The error matches ErrInvalid.
func (s Speed) Validate() error {
	_, err := ParseSpeed(string(s))
	return err
}

// UnmarshalText reads a speed name, so that a decoder refuses what no port has: the six configurable
// speeds and SpeedLinkDown (a reading reports it). An error matches ErrInvalid. The text of a Speed is
// the value itself, so nothing is needed to encode one.
func (s *Speed) UnmarshalText(text []byte) error {
	for _, known := range append(configurableSpeeds[:len(configurableSpeeds):len(configurableSpeeds)], SpeedLinkDown) {
		if string(known) == string(text) {
			*s = known
			return nil
		}
	}
	return invalidf("escp: unknown speed %q (valid: %v or %s)", text, configurableSpeeds, SpeedLinkDown)
}

// speedFromCode names a speed code of a read reply (0 = no link).
func speedFromCode(b byte) Speed {
	if b == 0 {
		return SpeedLinkDown
	}
	if int(b) <= len(configurableSpeeds) {
		return configurableSpeeds[b-1]
	}
	return Speed(fmt.Sprintf("unknown(%d)", b))
}

// code returns the wire code of a configurable speed.
func (s Speed) code() (byte, error) {
	for i, n := range configurableSpeeds {
		if n == s {
			return byte(i + 1), nil
		}
	}
	return 0, invalidf("escp: unknown speed %q (valid: %v)", string(s), configurableSpeeds)
}

// parsePorts decodes TLV 4096 values: port, state, lag, cfg speed, actual
// speed, cfg flow control, actual flow control.
func parsePorts(tlvs []wire.TLV) ([]Port, error) {
	var ports []Port
	for _, t := range tlvs {
		if t.Type != wire.TLVPorts {
			continue
		}
		v := t.Value
		if len(v) < portEntryLen {
			return nil, fmt.Errorf("escp: port TLV too short (%d bytes)", len(v))
		}
		ports = append(ports, Port{
			ID: PortID(v[portOffPort]), Enabled: v[portOffState] == 1, LAG: LAGID(v[portOffLAG]),
			ConfiguredSpeed: speedFromCode(v[portOffCfgSpeed]), ActualSpeed: speedFromCode(v[portOffActSpeed]),
			ConfiguredFlowControl: v[portOffCfgFlow] == 1, ActualFlowControl: v[portOffActFlow] == 1,
		})
	}
	return ports, nil
}

// Ports reads the Port Setting page: state, LAG membership, speed and flow control per port.
func (c *Client) Ports(ctx context.Context) ([]Port, error) {
	tlvs, err := c.getTLVs(ctx, wire.TLVPorts)
	if err != nil {
		return nil, err
	}
	return parsePorts(tlvs)
}

// PortConfig is what SetPort writes to one port. All fields are required: the switch
// takes the whole entry, so a caller changing one field passes the others as read.
// FlowControl can only be true with a speed for which Speed.AllowsFlowControl is true.
type PortConfig struct {
	ID          PortID
	Enabled     bool
	Speed       Speed // a configurable speed; SpeedLinkDown is refused
	FlowControl bool
}

// Validate checks the entry before it is written: a valid port, a configurable speed (SpeedLinkDown
// is only ever reported) and flow control only where the speed keeps it. The error matches
// ErrInvalid.
func (cfg PortConfig) Validate() error {
	if err := cfg.ID.Validate(); err != nil {
		return err
	}
	if _, err := cfg.Speed.code(); err != nil {
		return err
	}
	if cfg.FlowControl && !cfg.Speed.AllowsFlowControl() {
		return invalidf("escp: port %d: flow control needs speed %s or %s, not %s (the switch keeps it off at a fixed 10 or 100 Mbps speed)", cfg.ID, SpeedAuto, Speed1000Full, cfg.Speed)
	}
	return nil
}

// SetPort writes the configuration of one port: admin state, speed and flow
// control (7-byte TLV 4096 as sent by the vendor utility). Flow control can only be
// on with speed auto or 1000Mbps-full (see Speed.AllowsFlowControl): any other
// combination is refused before anything is sent, because the switch would accept
// it and silently keep flow control off. Changing a port that carries traffic can
// cut connectivity; callers must guard against that.
//
// For a port that is a member of a LAG the switch refuses half duplex (the error names
// that), but it accepts a different speed, flow control or admin state and so lets
// the members drift apart: keep them alike, or change the whole group.
func (c *Client) SetPort(ctx context.Context, cfg PortConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	code, _ := cfg.Speed.code() // Validate accepted the speed
	if err := c.requirePorts(ctx, cfg.ID.set()); err != nil {
		return err
	}
	entry := make([]byte, portEntryLen)
	entry[portOffPort], entry[portOffState], entry[portOffCfgSpeed], entry[portOffCfgFlow] = byte(cfg.ID), boolByte(cfg.Enabled), code, boolByte(cfg.FlowControl)
	return nameRefusal(c.setTLVs(ctx, Operation{Name: "SetPort", Ports: cfg.ID.set(), Impact: ImpactManagement}, wire.TLV{Type: wire.TLVPorts, Value: entry}), portRefusals)
}

// portRefusals names the refusals of a port write that were provoked on a real switch
// (firmware 20250710): half duplex on a port that is a member of a LAG. Half duplex on a
// port that is not a member is accepted.
var portRefusals = map[ReplyCode]string{
	lagRefusedHalfDuplex: "the port is a member of a LAG, and LAG members must be full duplex",
}
