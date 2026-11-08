package escp

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
)

func parseBandwidth(tlvs []wire.TLV) ([]model.PortBandwidth, error) {
	byPort := map[model.PortID]*model.PortBandwidth{}
	for _, t := range tlvs {
		if t.Type != wire.TLVBandwidthIngress && t.Type != wire.TLVBandwidthEgress {
			continue
		}
		if len(t.Value) != bwEntryLen {
			return nil, fmt.Errorf("escp: bandwidth entry has %d bytes, want %d", len(t.Value), bwEntryLen)
		}
		port, rate := model.PortID(t.Value[bwOffPort]), model.Kbps(binary.BigEndian.Uint32(t.Value[bwOffRate:]))
		pb := byPort[port]
		if pb == nil {
			pb = &model.PortBandwidth{Port: port}
			byPort[port] = pb
		}
		if t.Type == wire.TLVBandwidthIngress {
			pb.Ingress = rate
		} else {
			pb.Egress = rate
		}
	}
	out := make([]model.PortBandwidth, 0, len(byPort))
	for _, pb := range byPort {
		out = append(out, *pb)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out, nil
}

// Bandwidth reads the per-port ingress and egress rate limits.
func (c *Client) Bandwidth(ctx context.Context) ([]model.PortBandwidth, error) {
	in, err := c.getTLVs(ctx, wire.TLVBandwidthIngress)
	if err != nil {
		return nil, err
	}
	eg, err := c.getTLVs(ctx, wire.TLVBandwidthEgress)
	if err != nil {
		return nil, err
	}
	return parseBandwidth(append(in, eg...))
}

// Layout of a bandwidth entry (TLVs 12544/12545): port (1 byte), enabled flag
// (1 byte), rate in kbps (4 bytes).
const (
	bwOffPort    = 0
	bwOffEnabled = 1
	bwOffRate    = 2
	bwEntryLen   = bwOffRate + 4
)

// bandwidthTLV encodes one direction of a limit that PortBandwidth.Validate accepted.
func bandwidthTLV(typ wire.TLVType, port model.PortID, kbps model.Kbps) wire.TLV {
	v := make([]byte, bwEntryLen)
	v[bwOffPort] = byte(port)
	if kbps != 0 {
		v[bwOffEnabled] = 1
	}
	binary.BigEndian.PutUint32(v[bwOffRate:], uint32(kbps))
	return wire.TLV{Type: typ, Value: v}
}

// SetBandwidth sets a port's ingress and egress limits in kbps (0 = unlimited,
// otherwise a multiple of 64; the same model.PortBandwidth type Bandwidth reads). An ingress limit is refused (ErrInvalid) on a port
// whose storm control is on: the switch would turn storm control off without saying so. Like the vendor utility it sends two requests,
// ingress first, then egress. A limit on a port that carries traffic throttles
// it; callers must guard against that. The two requests are not atomic: if the second
// fails (or its gate refuses, or the call is canceled) the ingress limit stays applied.
func (c *Client) SetBandwidth(ctx context.Context, limit model.PortBandwidth) error {
	if err := limit.Validate(); err != nil {
		return err
	}
	in, eg := bandwidthTLV(wire.TLVBandwidthIngress, limit.Port, limit.Ingress), bandwidthTLV(wire.TLVBandwidthEgress, limit.Port, limit.Egress)
	if err := c.requirePorts(ctx, limit.Port.AsSet()); err != nil {
		return err
	}
	if limit.Ingress != 0 {
		if err := c.requireNoStorm(ctx, limit.Port, limit.Ingress); err != nil {
			return err
		}
	}
	op := model.MethodSetBandwidth.Operation().On(limit.Port.AsSet())
	if err := c.setTLVs(ctx, op, in); err != nil {
		return err
	}
	return c.setTLVs(ctx, op, eg)
}
