package escp

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/davidlukac/escp-go/internal/wire"
)

// Layout of a storm control entry.
const (
	stormOffPort           = 0
	stormOffEnabled        = 1
	stormOffUnknownUnicast = 2
	stormOffMulticast      = 3
	stormOffBroadcast      = 4
	stormOffRate           = 5
	stormEntryLen          = stormOffRate + 4
)

// PortStorm is a port's storm control setting: which kinds of traffic are limited and at what rate.
type PortStorm struct {
	Port           PortID
	UnknownUnicast bool // limit unknown-unicast traffic
	Multicast      bool // limit multicast traffic
	Broadcast      bool // limit broadcast traffic
	Rate           Kbps // the limit, a multiple of RateStep; 0 when none is set
	// Enabled mirrors the web UI: a rate is set and at least one traffic type is selected.
	Enabled bool
}

func parseStorm(tlvs []wire.TLV) ([]PortStorm, error) {
	var out []PortStorm
	for _, t := range tlvs {
		if t.Type != wire.TLVStormControl {
			continue
		}
		v := t.Value
		if len(v) != stormEntryLen {
			return nil, fmt.Errorf("escp: storm control entry has %d bytes, want %d", len(v), stormEntryLen)
		}
		p := PortStorm{Port: PortID(v[stormOffPort]), UnknownUnicast: v[stormOffUnknownUnicast] != 0, Multicast: v[stormOffMulticast] != 0,
			Broadcast: v[stormOffBroadcast] != 0, Rate: Kbps(binary.BigEndian.Uint32(v[stormOffRate:]))}
		p.Enabled = p.Rate != 0 && (p.UnknownUnicast || p.Multicast || p.Broadcast)
		out = append(out, p)
	}
	return out, nil
}

// StormControl reads the storm control setting of every port.
func (c *Client) StormControl(ctx context.Context) ([]PortStorm, error) {
	tlvs, err := c.getTLVs(ctx, wire.TLVStormControl)
	if err != nil {
		return nil, err
	}
	return parseStorm(tlvs)
}

// StormConfig is what SetStormControl writes to one port. Unlike PortStorm (a reading)
// it has no Enabled flag: storm control is on exactly when a traffic type is selected.
type StormConfig struct {
	Port           PortID
	UnknownUnicast bool
	Multicast      bool
	Broadcast      bool
	Rate           Kbps
}

// limited reports whether any traffic type is selected: storm control is on exactly then.
func (cfg StormConfig) limited() bool { return cfg.UnknownUnicast || cfg.Multicast || cfg.Broadcast }

// Validate checks the setting before it is written: a valid port, and either a traffic type with a
// rate (a multiple of RateStep, at least one step, at most MaxBandwidth) or no traffic type
// and no rate. The error matches ErrInvalid.
func (cfg StormConfig) Validate() error {
	if err := cfg.Port.Validate(); err != nil {
		return err
	}
	switch {
	case cfg.limited() && (cfg.Rate < RateStep || cfg.Rate.Validate() != nil):
		return invalidf("escp: storm control on port %d needs a rate that is a multiple of %d kbps between %d and %d, got %d", cfg.Port, RateStep, RateStep, MaxBandwidth, cfg.Rate)
	case !cfg.limited() && cfg.Rate != 0:
		return invalidf("escp: storm control on port %d has a rate (%d) but no traffic type selected", cfg.Port, cfg.Rate)
	}
	return nil
}

// SetStormControl configures storm control on one port. Following the vendor
// utility: selecting any traffic type requires a rate (a multiple of 64 kbps,
// at least 64, at most MaxBandwidth); selecting none disables it and the
// rate must be 0. A rate on a port that carries traffic drops its excess
// broadcast/multicast; callers must guard against that. Storm control is refused (ErrInvalid) on a port that has an
// ingress limit: the switch would remove the limit without saying so.
func (c *Client) SetStormControl(ctx context.Context, cfg StormConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	limited := cfg.limited()
	if err := c.requirePorts(ctx, cfg.Port.set()); err != nil {
		return err
	}
	if limited {
		if err := c.requireNoIngressLimit(ctx, cfg.Port); err != nil {
			return err
		}
	}
	v := make([]byte, stormEntryLen)
	v[stormOffPort] = byte(cfg.Port)
	v[stormOffEnabled] = boolByte(limited)
	v[stormOffUnknownUnicast], v[stormOffMulticast], v[stormOffBroadcast] = boolByte(cfg.UnknownUnicast), boolByte(cfg.Multicast), boolByte(cfg.Broadcast)
	binary.BigEndian.PutUint32(v[stormOffRate:], uint32(cfg.Rate))
	return c.setTLVs(ctx, Operation{Name: "SetStormControl", Ports: cfg.Port.set(), Impact: ImpactTraffic}, wire.TLV{Type: wire.TLVStormControl, Value: v})
}
