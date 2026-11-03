package escp

import (
	"context"
	"errors"

	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
)

// parseMTUVLAN decodes TLV 8192: enabled, uplink port. As in the vendor
// utility, a disabled MTU VLAN reports uplink port 1.
func parseMTUVLAN(tlvs []wire.TLV) (model.MTUVLAN, error) {
	for _, t := range tlvs {
		if t.Type == wire.TLVVLANMTU && len(t.Value) == 2 {
			m := model.MTUVLAN{Enabled: t.Value[0] == 1, UplinkPort: model.PortID(t.Value[1])}
			if !m.Enabled {
				m.UplinkPort = 1
			}
			return m, nil
		}
	}
	return model.MTUVLAN{}, errors.New("escp: no valid MTU VLAN TLV in reply")
}

// MTUVLAN reads whether the MTU VLAN mode is on and which port is its uplink.
func (c *Client) MTUVLAN(ctx context.Context) (model.MTUVLAN, error) {
	tlvs, err := c.getTLVs(ctx, wire.TLVVLANMTU)
	if err != nil {
		return model.MTUVLAN{}, err
	}
	return parseMTUVLAN(tlvs)
}
