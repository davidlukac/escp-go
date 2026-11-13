package escp

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
)

// tlvFloor is, for every TLV type a writer may send, the least impact class of changing it: the one place
// that fact is written. A writer whose request carries a TLV of a higher class than the one its row of the
// settings table declares tells its gate less than it does (DC-L07). A TLV type that is not here is one no
// writer may send yet: a writer that sends it fails the test below until someone decides its class, which
// is the point (the types the client only reads, the credentials, the key and the factory-default flag are
// left out on purpose). The class is the one `Impact` describes: management is what can cut the management
// path or the uplink (an address or DHCP, VLAN mode and membership, a port that is turned off or retuned,
// link aggregation, mirroring, loop prevention), traffic is what slows or drops some traffic without
// taking a port, an address or a VLAN away, and cosmetic is the rest.
var tlvFloor = map[wire.TLVType]Impact{
	wire.TLVHostname:         ImpactCosmetic,
	wire.TLVLED:              ImpactCosmetic,
	wire.TLVDHCP:             ImpactManagement,
	wire.TLVIP:               ImpactManagement,
	wire.TLVNetmask:          ImpactManagement,
	wire.TLVGateway:          ImpactManagement,
	wire.TLVPorts:            ImpactManagement,
	wire.TLVLAG:              ImpactManagement,
	wire.TLVMirror:           ImpactManagement,
	wire.TLVLoopPrevention:   ImpactManagement,
	wire.TLVVLANMTU:          ImpactManagement,
	wire.TLVVLANPortBased:    ImpactManagement,
	wire.TLVVLANPortBasedVID: ImpactManagement,
	wire.TLVVLAN8021Q:        ImpactManagement,
	wire.TLVVLAN8021QVLAN:    ImpactManagement,
	wire.TLVVLAN8021QPVID:    ImpactManagement,
	wire.TLVIGMPEnabled:      ImpactTraffic,
	wire.TLVIGMPSuppression:  ImpactTraffic,
	wire.TLVQoSMode:          ImpactTraffic,
	wire.TLVQoSPriority:      ImpactTraffic,
	wire.TLVStormControl:     ImpactTraffic,
	wire.TLVBandwidthIngress: ImpactTraffic,
	wire.TLVBandwidthEgress:  ImpactTraffic,
}

// resend is a writer whose request carries TLVs of a class above its own, and why that is allowed.
type resend struct {
	// types are the TLV types of the request that are above the writer's declared class: exactly those.
	types []wire.TLVType
	// why is the reason, for the reader of a failure.
	why string
}

// reassertsWhatItRead are the writers that send a TLV of a higher class than they declare because the
// switch takes a page as a whole, and so they send back what they have just read. It is allowed only
// while every such TLV carries, byte for byte, what the switch reported before the call: the request then
// asserts the address and the DHCP flag the switch already has, and changes nothing of how it is reached.
// The re-send is not a choice: the switch refuses a System-page SET that leaves out the DHCP flag or the
// gateway (error code 3, nothing changed; measured on firmware 20250710, see PROTOCOL.md).
// A writer that is not here has no excuse for a TLV above its class. Whether such a writer should declare
// the higher class instead is the question this table keeps in view (an owner decision, see the issue).
var reassertsWhatItRead = map[Method]resend{
	MethodSetHostname: {
		types: []wire.TLVType{wire.TLVDHCP, wire.TLVIP, wire.TLVNetmask, wire.TLVGateway},
		why:   "the System page is one request: the device description travels with the DHCP flag and the address triple the switch reported (discovery), unchanged",
	},
}

// reportedBefore reads, from what the switch reported before a call, the bytes a request that re-sends
// it must carry for the TLV type; a type that is not here cannot be excused as unchanged.
var reportedBefore = map[wire.TLVType]func(Info) []byte{
	wire.TLVDHCP:    func(i Info) []byte { return []byte{boolByte(i.DHCP)} },
	wire.TLVIP:      func(i Info) []byte { return i.IP.AsSlice() },
	wire.TLVNetmask: func(i Info) []byte { return i.Netmask.AsSlice() },
	wire.TLVGateway: func(i Info) []byte { return i.Gateway.AsSlice() },
}

// writerRow is one call of a writer and the TLV types its request or requests carry, as a set.
type writerRow struct {
	name   string
	method Method
	call   func(context.Context, *Client) error
	sends  []wire.TLVType
}

// writerRows is, for every writer of the settings table, what it sends: the documentation of the wire
// effect of each method, kept next to the check that holds it to that. The call of a writer is the one of
// the gate table (writeCalls); a writer whose request depends on its argument has one row for each kind
// of request. A new TLV type in a request fails the row until the table says so, and then until
// tlvFloor and the writer's declared class agree.
func writerRows() []writerRow {
	vlanMode := func(setting VLANModeSetting) func(context.Context, *Client) error {
		return func(ctx context.Context, c *Client) error { return c.SetVLANMode(ctx, setting) }
	}
	return []writerRow{
		{"SetHostname", MethodSetHostname, writeCalls[MethodSetHostname],
			[]wire.TLVType{wire.TLVHostname, wire.TLVDHCP, wire.TLVIP, wire.TLVNetmask, wire.TLVGateway}},
		{"SetIPConfig", MethodSetIPConfig, writeCalls[MethodSetIPConfig],
			[]wire.TLVType{wire.TLVHostname, wire.TLVDHCP, wire.TLVIP, wire.TLVNetmask, wire.TLVGateway}},
		{"EnableDHCP", MethodEnableDHCP, writeCalls[MethodEnableDHCP],
			[]wire.TLVType{wire.TLVHostname, wire.TLVDHCP, wire.TLVIP, wire.TLVNetmask, wire.TLVGateway}},
		{"SetLED", MethodSetLED, writeCalls[MethodSetLED], []wire.TLVType{wire.TLVLED}},
		{"SetPort", MethodSetPort, writeCalls[MethodSetPort], []wire.TLVType{wire.TLVPorts}},
		{"SetLAG", MethodSetLAG, writeCalls[MethodSetLAG], []wire.TLVType{wire.TLVLAG}},
		{"SetPortMirror", MethodSetPortMirror, writeCalls[MethodSetPortMirror], []wire.TLVType{wire.TLVMirror}},
		{"SetBandwidth", MethodSetBandwidth, writeCalls[MethodSetBandwidth],
			[]wire.TLVType{wire.TLVBandwidthIngress, wire.TLVBandwidthEgress}},
		{"SetStormControl", MethodSetStormControl, writeCalls[MethodSetStormControl], []wire.TLVType{wire.TLVStormControl}},
		{"SetQoSMode", MethodSetQoSMode, writeCalls[MethodSetQoSMode], []wire.TLVType{wire.TLVQoSMode}},
		{"SetPortPriorities", MethodSetPortPriorities, writeCalls[MethodSetPortPriorities], []wire.TLVType{wire.TLVQoSPriority}},
		{"SetIGMP", MethodSetIGMP, writeCalls[MethodSetIGMP], []wire.TLVType{wire.TLVIGMPEnabled, wire.TLVIGMPSuppression}},
		{"SetLoopPrevention", MethodSetLoopPrevention, writeCalls[MethodSetLoopPrevention], []wire.TLVType{wire.TLVLoopPrevention}},
		{"SetVLANMode mtu", MethodSetVLANMode, vlanMode(VLANModeSetting{Mode: VLANModeMTU, UplinkPort: 8}), []wire.TLVType{wire.TLVVLANMTU}},
		{"SetVLANMode port-based", MethodSetVLANMode, vlanMode(VLANModeSetting{Mode: VLANModePortBased}), []wire.TLVType{wire.TLVVLANPortBased}},
		{"SetVLANMode 802.1q", MethodSetVLANMode, vlanMode(VLANModeSetting{Mode: VLANModeDot1Q}), []wire.TLVType{wire.TLVVLAN8021Q}},
		{"SetPortBasedVLAN", MethodSetPortBasedVLAN, writeCalls[MethodSetPortBasedVLAN], []wire.TLVType{wire.TLVVLANPortBasedVID}},
		{"SetDot1QVLAN", MethodSetDot1QVLAN, writeCalls[MethodSetDot1QVLAN], []wire.TLVType{wire.TLVVLAN8021QVLAN}},
		{"DeleteDot1QVLAN", MethodDeleteDot1QVLAN, writeCalls[MethodDeleteDot1QVLAN], []wire.TLVType{wire.TLVVLAN8021QVLAN}},
		{"SetPVIDs", MethodSetPVIDs, writeCalls[MethodSetPVIDs], []wire.TLVType{wire.TLVVLAN8021QPVID}},
	}
}

// sentBy runs call against a client whose gate allows and returns the TLV types of every accepted SET as
// a sorted set, the settings of each SET, and what the switch reported just before the call.
func sentBy(t *testing.T, call func(context.Context, *Client) error) (types []wire.TLVType, sets [][]escptest.Setting, before Info) {
	t.Helper()
	f := escptest.Start(t)
	f.EnableModel(nil) // the pre-write read of the other limiter needs a switch that answers
	c := newBoundClient(t, Config{Host: "127.0.0.1", Username: "admin", Password: "secret", Timeout: 300 * time.Millisecond,
		SwitchPort: f.UDPPort(), BroadcastAddr: escptest.Loopback(),
		WriteGate: func(context.Context, Reader, Operation) error { return nil }}, withLoopback())
	t.Cleanup(func() { c.Close() })
	before, err := c.Info(context.Background())
	if err != nil {
		t.Fatalf("reading what the switch reports before the write: %v", err)
	}
	if err := call(context.Background(), c); err != nil {
		t.Fatalf("an allowed write failed: %v", err)
	}
	sets = f.AllSets()
	for _, set := range sets {
		for _, s := range set {
			if typ := wire.TLVType(s.Type); !slices.Contains(types, typ) {
				types = append(types, typ)
			}
		}
	}
	slices.Sort(types)
	return types, sets, before
}

// above is the TLV types of a request whose least class is higher than declared.
func above(types []wire.TLVType, declared Impact) []wire.TLVType {
	var out []wire.TLVType
	for _, typ := range types {
		if floor, known := tlvFloor[typ]; known && floor > declared {
			out = append(out, typ)
		}
	}
	return out
}

// sorted returns a sorted copy.
func sorted(types []wire.TLVType) []wire.TLVType {
	out := slices.Clone(types)
	slices.Sort(out)
	return out
}

// A writer sends what its row says, and nothing above its class: the impact a writer declares to the gate
// is checked against the bytes it puts on the wire, not against its name. Every request of every writer is
// recorded by the fake; the TLV types must be exactly the ones the table of writerRows lists (so a new
// type is a decision), each must have a class in tlvFloor, and none may need a class higher than the one the
// settings table declares for the writer, unless the writer is named in reassertsWhatItRead and the
// TLVs above its class carry exactly what the switch reported before.
//
// It does not check the values inside a TLV (apart from the unchanged ones of reassertsWhatItRead): a
// writer that sends a wrong port mask or rate is for the tests of that writer.
//
// guarantee: model.WriteGate
func TestWritersSendOnlyWhatTheirRowSays(t *testing.T) {
	for _, row := range writerRows() {
		t.Run(row.name, func(t *testing.T) {
			declared := registeredImpact(row.method)
			sent, sets, before := sentBy(t, row.call)
			if len(sets) == 0 {
				t.Fatal("the writer sent nothing")
			}
			if want := sorted(row.sends); !slices.Equal(sent, want) {
				t.Errorf("the request carries TLV types %v, the table lists %v: if the change is intended, say so in writerRows and decide the class of the new type in tlvFloor (the numbers are in internal/wire/tlv.go)", sent, want)
			}
			for _, typ := range sent {
				if _, known := tlvFloor[typ]; !known {
					t.Errorf("TLV type %d has no class in tlvFloor: decide the least impact of changing it before a writer sends it", typ)
				}
			}
			checkAboveItsClass(t, row.method, declared, sent, sets, before)
		})
	}
}

// checkAboveItsClass reports the TLV types of a request that need a higher class than the writer declares,
// except the ones its entry of reassertsWhatItRead excuses, and holds that entry to what it says.
func checkAboveItsClass(t *testing.T, method Method, declared Impact, sent []wire.TLVType, sets [][]escptest.Setting, before Info) {
	t.Helper()
	over := above(sent, declared)
	excuse, excused := reassertsWhatItRead[method]
	if !excused {
		for _, typ := range over {
			t.Errorf("%s declares %s but its request carries TLV type %d, which changes something of class %s or higher: declare the higher class, or send less", method, declared, typ, tlvFloor[typ])
		}
		return
	}
	if want := sorted(excuse.types); !slices.Equal(sorted(over), want) {
		t.Errorf("%s is excused for TLV types %v (%s) but the types above its class %s are %v: update reassertsWhatItRead", method, want, excuse.why, declared, over)
	}
	for _, typ := range excuse.types {
		reported, ok := reportedBefore[typ]
		if !ok {
			t.Errorf("TLV type %d is excused as unchanged but reportedBefore cannot say what the switch held", typ)
			continue
		}
		for i, set := range sets {
			for _, s := range set {
				if wire.TLVType(s.Type) == typ && string(s.Value) != string(reported(before)) {
					t.Errorf("%s declares %s and re-sends TLV type %d changed in SET %d: got %x, the switch reported %x", method, declared, typ, i+1, s.Value, reported(before))
				}
			}
		}
	}
}

// The table is complete and has no dead entry: every writer of the settings table has a row, every row is
// a writer of it, every TLV type of tlvFloor is sent by some row, every excuse names a writer and TLV types
// that exist, and a TLV type that is sent has a class.
func TestTheWriterRowsMatchTheSettingsTable(t *testing.T) {
	rows := writerRows()
	rowsOf := map[Method]int{}
	sentTypes := map[wire.TLVType]bool{}
	for _, row := range rows {
		rowsOf[row.method]++
		for _, typ := range row.sends {
			sentTypes[typ] = true
		}
	}
	written := 0
	for _, s := range Settings() {
		for _, w := range s.Writes {
			written++
			t.Run("writer "+string(w.Name), func(t *testing.T) {
				if rowsOf[w.Name] == 0 {
					t.Errorf("%s is a writer of the settings table but has no row in writerRows: drive it and list the TLV types it sends", w.Name)
				}
			})
		}
	}
	if written < 17 { // a floor against a broken table scan
		t.Fatalf("found only %d writers in the settings table; the scan is broken", written)
	}
	for _, row := range rows {
		t.Run("row "+row.name, func(t *testing.T) {
			if registeredImpact(row.method) == 0 {
				t.Errorf("%s is not a writer of the settings table: remove the row", row.method)
			}
			if row.call == nil {
				t.Errorf("%s has no call: writeCalls has no entry for it", row.name)
			}
		})
	}
	for typ := range tlvFloor {
		t.Run(fmt.Sprintf("class of TLV type %d", typ), func(t *testing.T) {
			if !sentTypes[typ] {
				t.Errorf("tlvFloor classes TLV type %d but no writer row sends it: remove it, or add the row of the writer that sends it", typ)
			}
		})
	}
	for method := range reassertsWhatItRead {
		t.Run("excuse "+string(method), func(t *testing.T) {
			if rowsOf[method] == 0 {
				t.Errorf("reassertsWhatItRead names %s, which has no row in writerRows: remove it", method)
			}
		})
	}
}
