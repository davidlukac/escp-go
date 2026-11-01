package escp

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/davidlukac/escp-go/internal/wire"
)

// PortBandwidth holds a port's rate limits in kbps; 0 means unlimited.
type PortBandwidth struct {
	Port    PortID
	Ingress Kbps
	Egress  Kbps
}

func parseBandwidth(tlvs []wire.TLV) ([]PortBandwidth, error) {
	byPort := map[PortID]*PortBandwidth{}
	for _, t := range tlvs {
		if t.Type != wire.TLVBandwidthIngress && t.Type != wire.TLVBandwidthEgress {
			continue
		}
		if len(t.Value) != bwEntryLen {
			return nil, fmt.Errorf("escp: bandwidth entry has %d bytes, want %d", len(t.Value), bwEntryLen)
		}
		port, rate := PortID(t.Value[bwOffPort]), Kbps(binary.BigEndian.Uint32(t.Value[bwOffRate:]))
		pb := byPort[port]
		if pb == nil {
			pb = &PortBandwidth{Port: port}
			byPort[port] = pb
		}
		if t.Type == wire.TLVBandwidthIngress {
			pb.Ingress = rate
		} else {
			pb.Egress = rate
		}
	}
	out := make([]PortBandwidth, 0, len(byPort))
	for _, pb := range byPort {
		out = append(out, *pb)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out, nil
}

// Bandwidth reads the per-port ingress and egress rate limits.
func (c *Client) Bandwidth(ctx context.Context) ([]PortBandwidth, error) {
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

// MaxBandwidthKbps is the largest limit the web UI offers.
const MaxBandwidthKbps = 1000000

// Validate checks the limits before they are written: a valid port and, in each direction, 0
// (unlimited) or a multiple of RateStepKbps up to MaxBandwidthKbps. The error matches ErrInvalid.
func (b PortBandwidth) Validate() error {
	if err := b.Port.Validate(); err != nil {
		return err
	}
	for _, rate := range []Kbps{b.Ingress, b.Egress} {
		if rate.Validate() != nil {
			return invalidf("escp: invalid rate %d kbps for port %d: use 0 (unlimited) or a multiple of %d up to %d", rate, b.Port, RateStepKbps, MaxBandwidthKbps)
		}
	}
	return nil
}

// bandwidthTLV encodes one direction of a limit that PortBandwidth.Validate accepted.
func bandwidthTLV(typ wire.TLVType, port PortID, kbps Kbps) wire.TLV {
	v := make([]byte, bwEntryLen)
	v[bwOffPort] = byte(port)
	if kbps != 0 {
		v[bwOffEnabled] = 1
	}
	binary.BigEndian.PutUint32(v[bwOffRate:], uint32(kbps))
	return wire.TLV{Type: typ, Value: v}
}

// SetBandwidth sets a port's ingress and egress limits in kbps (0 = unlimited,
// otherwise a multiple of 64; the same type Bandwidth reads). An ingress limit is refused (ErrInvalid) on a port
// whose storm control is on: the switch would turn storm control off without saying so. Like the vendor utility it sends two requests,
// ingress first, then egress. A limit on a port that carries traffic throttles
// it; callers must guard against that. The two requests are not atomic: if the second
// fails (or its gate refuses, or the call is cancelled) the ingress limit stays applied.
func (c *Client) SetBandwidth(ctx context.Context, limit PortBandwidth) error {
	if err := limit.Validate(); err != nil {
		return err
	}
	in, eg := bandwidthTLV(wire.TLVBandwidthIngress, limit.Port, limit.Ingress), bandwidthTLV(wire.TLVBandwidthEgress, limit.Port, limit.Egress)
	if err := c.requirePorts(ctx, limit.Port.set()); err != nil {
		return err
	}
	if limit.Ingress != 0 {
		if err := c.requireNoStorm(ctx, limit.Port, limit.Ingress); err != nil {
			return err
		}
	}
	op := Operation{Name: "SetBandwidth", Ports: limit.Port.set(), Impact: ImpactTraffic}
	if err := c.setTLVs(ctx, op, in); err != nil {
		return err
	}
	return c.setTLVs(ctx, op, eg)
}
