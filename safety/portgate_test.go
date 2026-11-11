package safety

import (
	"context"
	"errors"
	"testing"

	"github.com/davidlukac/escp-go/model"
)

// switchView is a switch as the gate can read it, counting the reads.
type switchView struct {
	model.Reader // the reads a gate does not make panic if they are made
	ports        []model.Port
	lags         []model.LAG
	pvlans       model.PortBasedVLAN
	dot1q        model.Dot1Q
	failWith     error  // the read named in failOn fails with it
	failOn       string // "Ports", "LAGs", "PortBasedVLAN" or "Dot1Q"
	reads        int
}

func (s *switchView) Info(context.Context) (model.Info, error) { return model.Info{}, nil }

// read counts a read and fails it when it is the one the test broke.
func (s *switchView) read(name string) error {
	s.reads++
	if s.failOn == name {
		return s.failWith
	}
	return nil
}

func (s *switchView) Ports(context.Context) ([]model.Port, error) { return s.ports, s.read("Ports") }
func (s *switchView) LAGs(context.Context) ([]model.LAG, error)   { return s.lags, s.read("LAGs") }
func (s *switchView) PortBasedVLAN(context.Context) (model.PortBasedVLAN, error) {
	return s.pvlans, s.read("PortBasedVLAN")
}
func (s *switchView) Dot1Q(context.Context) (model.Dot1Q, error) { return s.dot1q, s.read("Dot1Q") }

// view: port 1 is the uplink (linked, LAG 1), 2 is its idle partner, 3 and 4 are free, 5 and 7 are idle members of LAG 2.
func view() *switchView {
	return &switchView{
		ports: []model.Port{
			{ID: 1, ActualSpeed: "1000Mbps-full", LAG: 1},
			{ID: 2, ActualSpeed: model.SpeedLinkDown, LAG: 1},
			{ID: 3, ActualSpeed: model.SpeedLinkDown},
			{ID: 4, ActualSpeed: model.SpeedLinkDown},
			{ID: 5, ActualSpeed: model.SpeedLinkDown, LAG: 2},
			{ID: 6, ActualSpeed: "100Mbps-full"},
			{ID: 7, ActualSpeed: model.SpeedLinkDown, LAG: 2},
		},
		lags:   []model.LAG{{ID: 1, Ports: model.MustPortSet(1, 2)}, {ID: 2, Ports: model.MustPortSet(5, 7)}},
		pvlans: model.PortBasedVLAN{VLANs: []model.PortVLAN{{ID: 1, Ports: model.MustPortSet(1, 3, 4)}, {ID: 2, Ports: model.MustPortSet(3)}}},
		dot1q: model.Dot1Q{VLANs: []model.Dot1QVLAN{
			{ID: 1, Untagged: model.MustPortSet(1, 2, 3, 4)},
			{ID: 20, Tagged: model.MustPortSet(6), Untagged: model.MustPortSet(3)},
		}},
	}
}

// guarantee: PortGate
func TestPortGate(t *testing.T) {
	cases := []struct {
		name  string
		op    model.Operation
		want  *PortUseError // nil: allowed
		reads int           // how many reads the check is allowed to cost
	}{
		{"a write that names no port passes without a read", model.Operation{Name: model.MethodSetLED, Impact: model.ImpactCosmetic}, nil, 0},
		{"free ports pass", model.Operation{Name: model.MethodSetPort, Ports: model.MustPortSet(3, 4)}, nil, 1},
		{"a linked port is refused", model.Operation{Name: model.MethodSetPort, Ports: model.MustPortSet(6)}, &PortUseError{Port: 6, Reason: PortLinkUp, Speed: "100Mbps-full"}, 1},
		{"the uplink is refused by its link", model.Operation{Name: model.MethodSetPort, Ports: model.MustPortSet(1)}, &PortUseError{Port: 1, Reason: PortLinkUp, Speed: "1000Mbps-full", LAG: 1}, 1},
		{"a port the switch does not have", model.Operation{Name: model.MethodSetPort, Ports: model.MustPortSet(9)}, &PortUseError{Port: 9, Reason: PortMissing}, 1},
		{"an idle LAG member is refused when not editing its LAG", model.Operation{Name: model.MethodSetPort, Ports: model.MustPortSet(2)}, &PortUseError{Port: 2, Reason: PortInLAG, LAG: 1}, 1},

		{"a new LAG over free ports", model.Operation{Name: model.MethodSetLAG, Ports: model.MustPortSet(3, 4), Replaces: model.Group{LAG: 3}}, nil, 2},
		{"a LAG edit tolerates the group's own idle member", model.Operation{Name: model.MethodSetLAG, Ports: model.MustPortSet(5, 3), Replaces: model.Group{LAG: 2}}, nil, 2},
		{"removing a LAG whose member carries the uplink is refused", model.Operation{Name: model.MethodSetLAG, Replaces: model.Group{LAG: 1}}, &PortUseError{Port: 1, Reason: PortLinkUp, Speed: "1000Mbps-full", LAG: 1}, 2},
		{"replacing a LAG's members is refused for the member it drops", model.Operation{Name: model.MethodSetLAG, Ports: model.MustPortSet(3, 4), Replaces: model.Group{LAG: 1}}, &PortUseError{Port: 1, Reason: PortLinkUp, Speed: "1000Mbps-full", LAG: 1}, 2},
		{"a LAG edit does not tolerate another group's member", model.Operation{Name: model.MethodSetLAG, Ports: model.MustPortSet(2, 3), Replaces: model.Group{LAG: 2}}, &PortUseError{Port: 2, Reason: PortInLAG, LAG: 1}, 2},

		{"a port-based group over free ports", model.Operation{Name: model.MethodSetPortBasedVLAN, Ports: model.MustPortSet(3), Replaces: model.Group{PortVLAN: 2}}, nil, 2},
		{"removing the group that holds the uplink is refused", model.Operation{Name: model.MethodSetPortBasedVLAN, Replaces: model.Group{PortVLAN: 1}}, &PortUseError{Port: 1, Reason: PortLinkUp, Speed: "1000Mbps-full", LAG: 1}, 2},
		{"removing a group that has no members touches nothing", model.Operation{Name: model.MethodSetLAG, Replaces: model.Group{LAG: 3}}, nil, 1},
		{"a group that does not exist yet has no old members", model.Operation{Name: model.MethodSetPortBasedVLAN, Ports: model.MustPortSet(4), Replaces: model.Group{PortVLAN: 7}}, nil, 2},

		{"an 802.1Q VLAN that does not exist yet has no old members", model.Operation{Name: model.MethodSetDot1QVLAN, Ports: model.MustPortSet(4), Replaces: model.Group{Dot1QVLAN: 99}}, nil, 2},
		{"an 802.1Q VLAN without a linked member", model.Operation{Name: model.MethodSetDot1QVLAN, Ports: model.MustPortSet(4), Replaces: model.Group{Dot1QVLAN: 20}}, &PortUseError{Port: 6, Reason: PortLinkUp, Speed: "100Mbps-full"}, 2},
		{"rewriting VLAN 1 so that it drops the uplink is refused", model.Operation{Name: model.MethodSetDot1QVLAN, Ports: model.MustPortSet(3, 4), Replaces: model.Group{Dot1QVLAN: 1}}, &PortUseError{Port: 1, Reason: PortLinkUp, Speed: "1000Mbps-full", LAG: 1}, 2},
		{"a write that changes every port is refused for the first linked one", model.Operation{Name: model.MethodSetVLANMode, AllPorts: true}, &PortUseError{Port: 1, Reason: PortLinkUp, Speed: "1000Mbps-full", LAG: 1}, 1},
		{"a write that changes every port is refused although the port it names is free", model.Operation{Name: model.MethodSetVLANMode, Ports: model.MustPortSet(3), AllPorts: true}, &PortUseError{Port: 1, Reason: PortLinkUp, Speed: "1000Mbps-full", LAG: 1}, 1},
		{"deleting VLAN 1 names no port and is still refused", model.Operation{Name: model.MethodDeleteDot1QVLAN, Replaces: model.Group{Dot1QVLAN: 1}}, &PortUseError{Port: 1, Reason: PortLinkUp, Speed: "1000Mbps-full", LAG: 1}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sw := view()
			err := PortGate()(context.Background(), sw, tc.op)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("want it allowed, got %v", err)
				}
			} else {
				var got *PortUseError
				if !errors.As(err, &got) || *got != *tc.want {
					t.Fatalf("got %v, want %+v", err, *tc.want)
				}
			}
			if sw.reads > tc.reads {
				t.Errorf("the check read %d times, expected at most %d", sw.reads, tc.reads)
			}
		})
	}
}

// A gate that cannot look must refuse, whatever the reason.
func TestPortGateRefusesWhenItCannotLook(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("no reply")
	named := model.Operation{Name: model.MethodSetPort, Ports: model.MustPortSet(3)}
	t.Run("an operation that names nothing and replaces nothing reads nothing", func(t *testing.T) {
		sw := view()
		if err := PortGate()(ctx, sw, model.Operation{Name: model.MethodSetLED}); err != nil || sw.reads != 0 {
			t.Fatalf("got %v after %d reads", err, sw.reads)
		}
	})
	for _, tc := range []struct {
		reader string
		op     model.Operation
	}{
		{"Ports", named},
		{"Ports", model.Operation{Name: model.MethodSetLAG, Ports: model.MustPortSet(3, 4), Replaces: model.Group{LAG: 1}}}, // the groups read fine, the ports do not
		{"LAGs", model.Operation{Name: model.MethodSetLAG, Ports: model.MustPortSet(3, 4), Replaces: model.Group{LAG: 1}}},
		{"PortBasedVLAN", model.Operation{Name: model.MethodSetPortBasedVLAN, Ports: model.MustPortSet(3), Replaces: model.Group{PortVLAN: 2}}},
		{"Dot1Q", model.Operation{Name: model.MethodSetDot1QVLAN, Ports: model.MustPortSet(3), Replaces: model.Group{Dot1QVLAN: 20}}},
	} {
		op := tc.op
		t.Run("reading "+tc.reader+" fails for "+string(op.Name), func(t *testing.T) {
			sw := view()
			sw.failWith, sw.failOn = boom, tc.reader
			err := PortGate()(ctx, sw, op)
			if !errors.Is(err, ErrCannotCheckPorts) || !errors.Is(err, boom) {
				t.Fatalf("want ErrCannotCheckPorts wrapping the cause, got %v", err)
			}
		})
	}
	_ = named
}

// A Group that names several kinds is read for all of them: the answer fails closed.
func TestPortGateReadsEveryGroupAnOperationNames(t *testing.T) {
	sw := view()
	op := model.Operation{Name: "SetX", Replaces: model.Group{LAG: 2, PortVLAN: 1}}
	err := PortGate()(context.Background(), sw, op)
	var got *PortUseError
	if !errors.As(err, &got) || got.Port != 1 { // port 1 is in port-based group 1 and linked
		t.Fatalf("got %v, want port 1 refused", err)
	}
}

func TestPortGateUsesTheNamedPortsEvenWhenTheGroupIsEmpty(t *testing.T) {
	sw := view()
	sw.lags = nil // no LAG exists yet
	err := PortGate()(context.Background(), sw, model.Operation{Name: model.MethodSetLAG, Ports: model.MustPortSet(6, 3), Replaces: model.Group{LAG: 2}})
	var got *PortUseError
	if !errors.As(err, &got) || got.Port != 6 {
		t.Fatalf("got %v, want port 6 refused", err)
	}
}

// A write that changes every port passes on a switch that has no link and no LAG at all.
func TestPortGateLetsAWriteThatChangesEveryPortPassOnAnIdleSwitch(t *testing.T) {
	sw := &switchView{ports: []model.Port{{ID: 1, ActualSpeed: model.SpeedLinkDown}, {ID: 2, ActualSpeed: model.SpeedLinkDown}}}
	if err := PortGate()(context.Background(), sw, model.Operation{Name: model.MethodSetVLANMode, AllPorts: true}); err != nil {
		t.Fatalf("an idle switch: %v", err)
	}
	sw.ports[1].LAG = 1
	var got *PortUseError
	if err := PortGate()(context.Background(), sw, model.Operation{Name: model.MethodSetVLANMode, AllPorts: true}); !errors.As(err, &got) || got.Port != 2 || got.Reason != PortInLAG {
		t.Fatalf("a port in a LAG: got %v", err)
	}
}
