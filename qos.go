package escp

import (
	"context"
	"errors"
	"fmt"

	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
)

func parseQoS(tlvs []wire.TLV) (model.QoS, error) {
	out := model.QoS{Priorities: []model.PortPriority{}}
	seen := false
	for _, t := range tlvs {
		switch t.Type {
		case wire.TLVQoSMode:
			if len(t.Value) != 1 {
				return out, fmt.Errorf("escp: QoS mode TLV has %d bytes", len(t.Value))
			}
			out.Mode, seen = model.QoSMode(t.Value[0]), true
		case wire.TLVQoSPriority:
			if len(t.Value) != 2 {
				return out, fmt.Errorf("escp: QoS priority entry has %d bytes, want 2", len(t.Value))
			}
			out.Priorities = append(out.Priorities, model.PortPriority{Port: model.PortID(t.Value[0]), Priority: model.Priority(t.Value[1]) + model.PriorityLowest})
		}
	}
	if !seen {
		return out, errors.New("escp: no QoS mode in reply")
	}
	return out, nil
}

// QoS reads the QoS mode and per-port priorities. Priorities are only
// effective in port-based mode but are always reported.
func (c *Client) QoS(ctx context.Context) (model.QoS, error) {
	mode, err := c.getTLVs(ctx, wire.TLVQoSMode)
	if err != nil {
		return model.QoS{}, err
	}
	pri, err := c.getTLVs(ctx, wire.TLVQoSPriority)
	if err != nil {
		return model.QoS{}, err
	}
	return parseQoS(append(mode, pri...))
}

// SetQoSMode selects the QoS mode, which changes how traffic is queued on every port. The
// mode must be one of QoSModes. Per-port priorities (SetPortPriorities) only take effect in
// QoSPortBased; the switch accepts them in the other modes and ignores them.
func (c *Client) SetQoSMode(ctx context.Context, mode model.QoSMode) error {
	if err := mode.Validate(); err != nil {
		return err
	}
	return c.setTLVs(ctx, model.MethodSetQoSMode.Operation(), wire.TLV{Type: wire.TLVQoSMode, Value: []byte{byte(mode)}})
}

// SetPortPriorities changes the priority of the given ports, the same PortPriority entries QoS
// reads. A port may appear once; every port must exist and every entry be valid (PortPriority.Validate),
// or nothing is sent, and the first fault is reported in the order given. As in the vendor utility the
// request carries the priority of every port, so the current values are read first (a QoS read, which
// the write gate does not see) and the ports not mentioned are re-sent unchanged. The priorities only
// take effect in QoSPortBased mode. An empty list is refused: it would send a write that changes nothing.
func (c *Client) SetPortPriorities(ctx context.Context, changes []model.PortPriority) error {
	if len(changes) == 0 {
		return invalidf("escp: no priorities to set")
	}
	var named model.PortSet
	for _, ch := range changes {
		if err := ch.Validate(); err != nil {
			return err
		}
		if named.Has(ch.Port) {
			return invalidf("escp: port %d appears twice in the priorities", ch.Port)
		}
		named = named.Union(ch.Port.AsSet())
	}
	end, err := c.beginModify(ctx)
	if err != nil {
		return err
	}
	defer end()
	cur, err := c.QoS(ctx)
	if err != nil {
		return err
	}
	prio := map[model.PortID]model.Priority{}
	for _, p := range cur.Priorities {
		prio[p.Port] = p.Priority
	}
	for _, ch := range changes {
		if _, ok := prio[ch.Port]; !ok {
			return invalidf("escp: port %d does not exist", ch.Port)
		}
		prio[ch.Port] = ch.Priority
	}
	tlvs := make([]wire.TLV, 0, len(prio))
	for _, p := range cur.Priorities { // keep the switch's port order
		tlvs = append(tlvs, wire.TLV{Type: wire.TLVQoSPriority, Value: []byte{byte(p.Port), byte(prio[p.Port] - model.PriorityLowest)}})
	}
	return c.setTLVs(ctx, model.MethodSetPortPriorities.Operation().On(named), tlvs...)
}
