package escp

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
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

func parseStorm(tlvs []wire.TLV) ([]model.PortStorm, error) {
	var out []model.PortStorm
	for _, t := range tlvs {
		if t.Type != wire.TLVStormControl {
			continue
		}
		v := t.Value
		if len(v) != stormEntryLen {
			return nil, fmt.Errorf("escp: storm control entry has %d bytes, want %d", len(v), stormEntryLen)
		}
		p := model.PortStorm{Port: model.PortID(v[stormOffPort]), UnknownUnicast: v[stormOffUnknownUnicast] != 0, Multicast: v[stormOffMulticast] != 0,
			Broadcast: v[stormOffBroadcast] != 0, Rate: model.Kbps(binary.BigEndian.Uint32(v[stormOffRate:]))}
		p.Enabled = p.Rate != 0 && (p.UnknownUnicast || p.Multicast || p.Broadcast)
		out = append(out, p)
	}
	return out, nil
}

// StormControl reads the storm control setting of every port.
func (c *Client) StormControl(ctx context.Context) ([]model.PortStorm, error) {
	tlvs, err := c.getTLVs(ctx, wire.TLVStormControl)
	if err != nil {
		return nil, err
	}
	return parseStorm(tlvs)
}

// SetStormControl configures storm control on one port. Following the vendor
// utility: selecting any traffic type requires a rate (a multiple of 64 kbps,
// at least 64, at most MaxBandwidth); selecting none disables it and the
// rate must be 0. A rate on a port that carries traffic drops its excess
// broadcast/multicast; callers must guard against that. Storm control is refused (ErrInvalid) on a port that has an
// ingress limit: the switch would remove the limit without saying so.
func (c *Client) SetStormControl(ctx context.Context, cfg model.StormConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	limited := cfg.Limited()
	if err := c.requirePorts(ctx, cfg.Port.AsSet()); err != nil {
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
	return c.setTLVs(ctx, model.MethodSetStormControl.Operation().On(cfg.Port.AsSet()), wire.TLV{Type: wire.TLVStormControl, Value: v})
}
