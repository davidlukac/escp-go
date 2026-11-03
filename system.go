package escp

import (
	"context"
	"errors"

	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
)

func parseSystem(ports, auto []wire.TLV) (model.System, error) {
	var s model.System
	found := 0
	for _, t := range ports {
		if t.Type == wire.TLVPortCount && len(t.Value) == 1 {
			s.PortCount, found = int(t.Value[0]), found+1
		}
	}
	for _, t := range auto {
		if t.Type == wire.TLVAutoSave && len(t.Value) == 1 {
			s.AutoSave, found = t.Value[0] == 1, found+1
		}
	}
	if found != 2 {
		return s, errors.New("escp: incomplete system reply")
	}
	return s, nil
}

// System reads the port count and the auto-save flag.
func (c *Client) System(ctx context.Context) (model.System, error) {
	ports, err := c.getTLVs(ctx, wire.TLVPortCount)
	if err != nil {
		return model.System{}, err
	}
	auto, err := c.getTLVs(ctx, wire.TLVAutoSave)
	if err != nil {
		return model.System{}, err
	}
	return parseSystem(ports, auto)
}

// SetHostname changes the device description. Like the vendor utility, the
// request carries the current IP settings (read via discovery) unchanged, so
// the management address is re-asserted as it is. Verified live on firmware
// 20230218 and 20250710.
func (c *Client) SetHostname(ctx context.Context, name model.Hostname) error {
	if err := name.Validate(); err != nil {
		return err
	}
	info, err := c.Info(ctx)
	if err != nil {
		return err
	}
	cur, err := ipConfigFromInfo(info)
	if err != nil {
		return err
	}
	_, err = c.writeSystem(ctx, model.MethodSetHostname.Operation(), name, info.DHCP, cur)
	return err
}

// writeSystem sends the System-page SET: device description, DHCP flag and the
// static address triple. The switch takes all of them in one request.
func (c *Client) writeSystem(ctx context.Context, op model.Operation, name model.Hostname, dhcp bool, cfg model.IPConfig) (sent bool, err error) {
	return c.setTLVsSent(ctx, op,
		wire.TLV{Type: wire.TLVHostname, Value: append([]byte(name), 0)},
		wire.TLV{Type: wire.TLVDHCP, Value: []byte{boolByte(dhcp)}},
		wire.TLV{Type: wire.TLVIP, Value: cfg.IP.AsSlice()}, wire.TLV{Type: wire.TLVNetmask, Value: cfg.Netmask.AsSlice()}, wire.TLV{Type: wire.TLVGateway, Value: cfg.Gateway.AsSlice()})
}
