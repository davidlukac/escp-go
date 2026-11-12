package model

import (
	"slices"
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
// speeds, SpeedLinkDown (a reading reports it) and the empty text, which is the zero Speed (no speed
// set, or none read), so that the zero Port, PortConfig and PortStats read back from their own
// encoding. Validate still refuses the zero Speed as a setting. An error matches ErrInvalid. The text
// of a Speed is the value itself, so nothing is needed to encode one.
func (s *Speed) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		*s = ""
		return nil
	}
	for _, known := range append(configurableSpeeds[:len(configurableSpeeds):len(configurableSpeeds)], SpeedLinkDown) {
		if string(known) == string(text) {
			*s = known
			return nil
		}
	}
	return invalidf("escp: unknown speed %q (valid: %v or %s)", text, configurableSpeeds, SpeedLinkDown)
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
	if !slices.Contains(configurableSpeeds[:], cfg.Speed) {
		return invalidf("escp: unknown speed %q (valid: %v)", string(cfg.Speed), configurableSpeeds)
	}
	if cfg.FlowControl && !cfg.Speed.AllowsFlowControl() {
		return invalidf("escp: port %d: flow control needs speed %s or %s, not %s (the switch keeps it off at a fixed 10 or 100 Mbps speed)", cfg.ID, SpeedAuto, Speed1000Full, cfg.Speed)
	}
	return nil
}
