package escp

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
)

// Layout of a port statistics entry: port, state, link speed (1 byte each),
// then four 4-byte counters.
const (
	statsOffPort   = 0
	statsOffState  = 1
	statsOffLink   = 2
	statsOffTxGood = 3
	statsOffTxBad  = statsOffTxGood + 4
	statsOffRxGood = statsOffTxBad + 4
	statsOffRxBad  = statsOffRxGood + 4
	statsEntryLen  = statsOffRxBad + 4
)

// parsePortStats decodes TLV 16384: port, state, link, tx good, tx bad, rx good, rx bad (4 bytes each).
func parsePortStats(tlvs []wire.TLV) ([]model.PortStats, error) {
	var out []model.PortStats
	for _, t := range tlvs {
		if t.Type != wire.TLVPortStats {
			continue
		}
		v := t.Value
		if len(v) < statsEntryLen {
			return nil, fmt.Errorf("escp: port statistics TLV too short (%d bytes)", len(v))
		}
		out = append(out, model.PortStats{
			ID: model.PortID(v[statsOffPort]), Enabled: v[statsOffState] == 1, Link: speedFromCode(v[statsOffLink]),
			TxGood: binary.BigEndian.Uint32(v[statsOffTxGood:]), TxBad: binary.BigEndian.Uint32(v[statsOffTxBad:]),
			RxGood: binary.BigEndian.Uint32(v[statsOffRxGood:]), RxBad: binary.BigEndian.Uint32(v[statsOffRxBad:]),
		})
	}
	return out, nil
}

// PortStats reads the traffic counters of every port. The counters change on every read, so
// they do not belong in anything compared between two reads.
func (c *Client) PortStats(ctx context.Context) ([]model.PortStats, error) {
	tlvs, err := c.getTLVs(ctx, wire.TLVPortStats)
	if err != nil {
		return nil, err
	}
	return parsePortStats(tlvs)
}
