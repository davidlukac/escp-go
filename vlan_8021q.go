package escp

import (
	"context"
	"errors"
	"fmt"

	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
)

func parseDot1Q(tlvs []wire.TLV) (model.Dot1Q, error) {
	out := model.Dot1Q{VLANs: []model.Dot1QVLAN{}, PVIDs: []model.PortPVID{}}
	seen := false
	for _, t := range tlvs {
		v := t.Value
		switch t.Type {
		case wire.TLVVLAN8021Q:
			if len(v) != 1 {
				return out, fmt.Errorf("escp: 802.1Q state TLV has %d bytes", len(v))
			}
			out.Enabled, seen = v[0] == 1, true
		case wire.TLVVLAN8021QMax:
			if len(v) == 1 {
				out.MaxVLANs = int(v[0])
			}
		case wire.TLVVLAN8021QVLAN:
			if len(v) < vlan8021QOffName {
				return out, fmt.Errorf("escp: 802.1Q VLAN entry has %d bytes, want at least %d", len(v), vlan8021QOffName)
			}
			// Vendor semantics: member and tagged-mask -> tagged; member only -> untagged.
			members, taggedMask := maskAt(v, vlan8021QOffMember), maskAt(v, vlan8021QOffTagged)
			untagged := members.Minus(taggedMask)
			out.VLANs = append(out.VLANs, model.Dot1QVLAN{ID: model.VLANID(int(v[0])<<8 | int(v[1])), Name: model.VLANName(wire.CString(v[vlan8021QOffName:])),
				Tagged: members.Minus(untagged), Untagged: untagged})
		case wire.TLVVLAN8021QPVID:
			if len(v) != pvidEntryLen {
				return out, fmt.Errorf("escp: 802.1Q PVID entry has %d bytes, want %d", len(v), pvidEntryLen)
			}
			out.PVIDs = append(out.PVIDs, model.PortPVID{Port: model.PortID(v[0]), PVID: model.VLANID(int(v[1])<<8 | int(v[2]))})
		}
	}
	if !seen {
		return out, errors.New("escp: no 802.1Q state in reply")
	}
	return out, nil
}

// Dot1Q reads the 802.1Q mode, VLAN table and per-port PVIDs.
func (c *Client) Dot1Q(ctx context.Context) (model.Dot1Q, error) {
	// Separate requests, like the vendor utility (combined ones are rejected on some pages).
	vlans, err := c.getTLVs(ctx, wire.TLVVLAN8021QVLAN)
	if err != nil {
		return model.Dot1Q{}, err
	}
	pvids, err := c.getTLVs(ctx, wire.TLVVLAN8021QPVID)
	if err != nil {
		return model.Dot1Q{}, err
	}
	return parseDot1Q(append(vlans, pvids...))
}
