package safety

import (
	"context"
	"errors"
	"fmt"

	"github.com/davidlukac/escp-go/model"
)

// ErrCannotCheckPorts is what PortGate returns when it cannot establish what the ports are used
// for: reading them failed. The write is refused,
// because a gate that cannot look must not wave a write through.
var ErrCannotCheckPorts = errors.New("safety: cannot check what the ports are used for")

// PortGate returns a gate that refuses a write unless every port it touches is free: it must
// exist, have no link and belong to no LAG (CheckPorts, with the same *PortUseError). It looks
// at the ports the operation names and, for a write that replaces or removes a group of ports
// (a LAG, a port-based VLAN, an 802.1Q VLAN), at the group's current members too, because
// taking the uplink out of VLAN 1 is as dangerous as naming it. A LAG's own idle members are
// tolerated when the LAG itself is edited; a link never is. A write that changes every port
// (Operation.AllPorts: the VLAN mode) touches all of them, so it passes only on a switch with no
// link and no LAG at all. Operations that touch no port pass without a read, and that includes
// writes that can cut the management path without naming a port (a new address or DHCP,
// loop prevention): the gate answers
// "is this port free?", MaxImpact answers "what kind of change is this?", and a front end
// that offers those writes needs a confirmation of its own for them.
//
// It reads through the client it guards (the gate is handed the client, which has the readers),
// before every write it checks, so it costs reads and is for a front end that wants the rule
// enforced by the library instead of by its own code. A front end with an opt-in leaves it out.
// Failures to look are refused with ErrCannotCheckPorts.
func PortGate() model.WriteGate {
	return func(ctx context.Context, sw model.Reader, op model.Operation) error {
		touched := op.Ports
		if op.Replaces.IsZero() && touched.IsEmpty() && !op.AllPorts {
			return nil
		}
		members, err := membersOf(ctx, sw, op.Replaces)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrCannotCheckPorts, err)
		}
		touched = touched.Union(members)
		if touched.IsEmpty() && !op.AllPorts {
			return nil
		}
		all, err := sw.Ports(ctx)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrCannotCheckPorts, err)
		}
		if op.AllPorts { // a write that changes every port touches every port the switch has
			for _, p := range all {
				touched = touched.Union(p.ID.AsSet())
			}
		}
		return CheckPorts(all, PortRule{TolerateLAG: op.Replaces.LAG}, touched)
	}
}

// membersOf lists the ports that belong to the group now (none for no group, or one that does not
// exist yet). A Group that names more than one kind is read for all of them, so the answer fails closed.
func membersOf(ctx context.Context, r model.Reader, g model.Group) (model.PortSet, error) {
	var members model.PortSet
	for _, read := range []struct {
		named bool
		read  func() (model.PortSet, error)
	}{
		{g.LAG != 0, func() (model.PortSet, error) { return lagMembers(ctx, r, g.LAG) }},
		{g.PortVLAN != 0, func() (model.PortSet, error) { return portVLANMembers(ctx, r, g.PortVLAN) }},
		{g.Dot1QVLAN != 0, func() (model.PortSet, error) { return dot1qMembers(ctx, r, g.Dot1QVLAN) }},
	} {
		if !read.named {
			continue
		}
		ports, err := read.read()
		if err != nil {
			return 0, err
		}
		members = members.Union(ports)
	}
	return members, nil
}

func lagMembers(ctx context.Context, r model.Reader, id model.LAGID) (model.PortSet, error) {
	lags, err := r.LAGs(ctx)
	if err != nil {
		return 0, err
	}
	for _, l := range lags {
		if l.ID == id {
			return l.Ports, nil
		}
	}
	return 0, nil
}

func portVLANMembers(ctx context.Context, r model.Reader, id model.PortVLANID) (model.PortSet, error) {
	v, err := r.PortBasedVLAN(ctx)
	if err != nil {
		return 0, err
	}
	if ports, ok := v.Group(id); ok {
		return ports, nil
	}
	return 0, nil
}

func dot1qMembers(ctx context.Context, r model.Reader, id model.VLANID) (model.PortSet, error) {
	q, err := r.Dot1Q(ctx)
	if err != nil {
		return 0, err
	}
	for _, v := range q.VLANs {
		if v.ID == id {
			return v.Tagged.Union(v.Untagged), nil
		}
	}
	return 0, nil
}
