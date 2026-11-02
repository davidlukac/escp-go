package escp

import (
	"context"
	"errors"

	"github.com/davidlukac/escp-go/internal/wire"
)

// LoopPrevention reports whether loop prevention (Monitoring > Loop Prevention) is on.
func (c *Client) LoopPrevention(ctx context.Context) (bool, error) {
	tlvs, err := c.getTLVs(ctx, wire.TLVLoopPrevention)
	if err != nil {
		return false, err
	}
	return parseLoopPrevention(tlvs)
}

func parseLoopPrevention(tlvs []wire.TLV) (bool, error) {
	for _, t := range tlvs {
		if t.Type == wire.TLVLoopPrevention && len(t.Value) == 1 {
			return t.Value[0] == 1, nil
		}
	}
	return false, errors.New("escp: no loop prevention TLV in reply")
}

// SetLoopPrevention enables or disables loop prevention. With it off the switch no longer
// detects a cabling loop, which can flood the network; turning it on is the safe direction.
// Toggling it was verified on a live switch without interrupting traffic.
func (c *Client) SetLoopPrevention(ctx context.Context, on bool) error {
	return c.setTLVs(ctx, Operation{Name: "SetLoopPrevention", Impact: impactOf("SetLoopPrevention")}, wire.TLV{Type: wire.TLVLoopPrevention, Value: []byte{boolByte(on)}})
}
