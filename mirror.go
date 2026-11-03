package escp

import (
	"context"
	"errors"
	"fmt"

	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
)

// Layout of the mirror entry: enabled flag, destination port, ingress mask, egress mask.
const (
	mirrorOffEnabled = 0
	mirrorOffPort    = 1
	mirrorOffIngress = 2
	mirrorOffEgress  = mirrorOffIngress + maskLen
	mirrorEntryLen   = mirrorOffEgress + maskLen
)

// parseMirror decodes TLV 16640: enabled, destination port, ingress mask(4), egress mask(4).
// When disabled the switch zeroes the rest, and so do we.
func parseMirror(tlvs []wire.TLV) (model.Mirror, error) {
	for _, t := range tlvs {
		if t.Type != wire.TLVMirror {
			continue
		}
		if len(t.Value) != mirrorEntryLen {
			return model.Mirror{}, fmt.Errorf("escp: port mirror TLV has %d bytes, want %d", len(t.Value), mirrorEntryLen)
		}
		m := model.Mirror{}
		if t.Value[mirrorOffEnabled] == 1 {
			m.Enabled, m.Destination = true, model.PortID(t.Value[mirrorOffPort])
			m.IngressPorts = maskAt(t.Value, mirrorOffIngress)
			m.EgressPorts = maskAt(t.Value, mirrorOffEgress)
		}
		return m, nil
	}
	return model.Mirror{}, errors.New("escp: no port mirror TLV in reply")
}

// PortMirror reads the port mirroring configuration: whether it is on, the monitoring port and the
// source ports per direction.
func (c *Client) PortMirror(ctx context.Context) (model.Mirror, error) {
	tlvs, err := c.getTLVs(ctx, wire.TLVMirror)
	if err != nil {
		return model.Mirror{}, err
	}
	return parseMirror(tlvs)
}

// SetPortMirror writes the mirroring configuration (10-byte TLV 16640). To
// disable, pass the zero Mirror. The destination port stops carrying normal
// traffic; callers must guard against choosing a port that is in use.
func (c *Client) SetPortMirror(ctx context.Context, m model.Mirror) error {
	if err := m.Validate(); err != nil {
		return err
	}
	op := model.MethodSetPortMirror.Operation()
	v := make([]byte, mirrorEntryLen)
	if m.Enabled {
		op.Ports = mirrorPorts(m)
		if err := c.requirePorts(ctx, op.Ports); err != nil {
			return err
		}
		v[mirrorOffEnabled], v[mirrorOffPort] = 1, byte(m.Destination)
		copy(v[mirrorOffIngress:], m.IngressPorts.Mask())
		copy(v[mirrorOffEgress:], m.EgressPorts.Mask())
	}
	return c.setTLVs(ctx, op, wire.TLV{Type: wire.TLVMirror, Value: v})
}

// mirrorPorts is every port the configuration mentions: the sources and the destination.
func mirrorPorts(m model.Mirror) model.PortSet {
	return m.IngressPorts.Union(m.EgressPorts).Union(m.Destination.AsSet())
}
