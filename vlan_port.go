package escp

import (
	"context"
	"errors"
	"fmt"

	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
)

// Layout of a port-based VLAN entry (TLV 8449): VLAN id (1 byte), member port mask (maskLen bytes).
const (
	pvlanOffMask  = 1
	pvlanEntryLen = pvlanOffMask + maskLen
)

func parsePortBasedVLAN(tlvs []wire.TLV) (model.PortBasedVLAN, error) {
	out := model.PortBasedVLAN{VLANs: []model.PortVLAN{}}
	seen := false
	for _, t := range tlvs {
		switch t.Type {
		case wire.TLVVLANPortBased:
			if len(t.Value) != 1 {
				return out, fmt.Errorf("escp: port-based VLAN state TLV has %d bytes", len(t.Value))
			}
			out.Enabled, seen = t.Value[0] == 1, true
		case wire.TLVVLANPortBasedMax:
			if len(t.Value) == 1 {
				out.MaxGroups = int(t.Value[0])
			}
		case wire.TLVVLANPortBasedVID:
			if len(t.Value) != pvlanEntryLen {
				return out, fmt.Errorf("escp: port-based VLAN entry has %d bytes, want %d", len(t.Value), pvlanEntryLen)
			}
			out.VLANs = append(out.VLANs, model.PortVLAN{ID: model.PortVLANID(t.Value[0]), Ports: maskAt(t.Value, pvlanOffMask)})
		}
	}
	if !seen {
		return out, errors.New("escp: no port-based VLAN state in reply")
	}
	return out, nil
}

// PortBasedVLAN reads the port-based VLAN mode and groups.
func (c *Client) PortBasedVLAN(ctx context.Context) (model.PortBasedVLAN, error) {
	tlvs, err := c.getTLVs(ctx, wire.TLVVLANPortBased)
	if err != nil {
		return model.PortBasedVLAN{}, err
	}
	return parsePortBasedVLAN(tlvs)
}

// SetPortBasedVLAN creates or replaces a port-based VLAN group: TLV 8449 with
// the VLAN id (1 byte) and the member port mask. An empty port set removes
// the group. The mode itself (enabled/disabled) is not touched. Changing a
// group whose ports carry traffic can cut connectivity; callers must guard.
func (c *Client) SetPortBasedVLAN(ctx context.Context, group model.PortVLAN) error {
	if err := group.Validate(); err != nil {
		return err
	}
	if err := c.requirePortVLANGroup(ctx, group.ID); err != nil {
		return err
	}
	if err := c.requirePorts(ctx, group.Ports); err != nil {
		return err
	}
	return c.setTLVs(ctx, model.MethodSetPortBasedVLAN.Operation().On(group.Ports).Replacing(model.Group{PortVLAN: group.ID}), wire.TLV{Type: wire.TLVVLANPortBasedVID, Value: append([]byte{byte(group.ID)}, group.Ports.Mask()...)})
}

// requirePortVLANGroup refuses a group id the switch does not have. The limit is read once and kept,
// in the same way as the port count, so the first group write costs the reads of the group state
// and of the port count; if one fails, so does the write, with that error. The caller has checked
// the id's own range (PortVLAN.Validate) before anything is read.
func (c *Client) requirePortVLANGroup(ctx context.Context, id model.PortVLANID) error {
	limit := model.PortVLANLimit(c.groupLimit.Load())
	if !limit.Known() { // not read yet: a read always yields a known limit, as the port count must be reported
		v, err := c.PortBasedVLAN(ctx)
		if err != nil {
			return err
		}
		ports, err := c.portCount(ctx)
		if err != nil {
			return err
		}
		limit = v.GroupLimit(model.System{PortCount: ports})
		c.groupLimit.Store(int64(limit))
	}
	return limit.Check(id)
}
