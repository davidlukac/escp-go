package model

import (
	"strconv"
)

// LAG limits of the TL-SG108E. They come from the switch's own web UI
// (PortTrunkRpm.htm: "available ports of LAG 1: port 1 -- port 4", "LAG 2:
// port 5 -- port 8", at most 4 ports, at least 2) and were confirmed on the
// wire on firmware 20230218 and 20250710: a mask outside the group's range is
// refused with code 4.
const (
	// LAGGroupCount is the number of LAG groups.
	LAGGroupCount = 2
	// LAGPortsPerGroup is how many ports each group owns (group 1: ports 1-4, group 2: 5-8) and the most it can hold.
	LAGPortsPerGroup = 4
	// LAGMinPorts is the fewest members a group can have (a single port is refused with code 7415).
	LAGMinPorts = 2
)

// LAGID is the number of a link aggregation group, 1 to LAGGroupCount. The zero value means "no
// group" where a record allows it (a port that is in none).
type LAGID int

// Validate reports whether g names a group of this switch family; the error matches ErrInvalid.
func (g LAGID) Validate() error {
	if g < 1 || g > LAGGroupCount {
		return invalidf("escp: invalid LAG group %d (valid: 1-%d)", g, LAGGroupCount)
	}
	return nil
}

// String is the group number.
func (g LAGID) String() string { return strconv.Itoa(int(g)) }

// Range is the first and the last port the group owns (group 1: ports 1-4, group 2: ports 5-8);
// g must be valid.
func (g LAGID) Range() (first, last PortID) {
	return PortID(int(g-1)*LAGPortsPerGroup + 1), PortID(int(g) * LAGPortsPerGroup)
}

// Validate checks the group number and the member ports against the limits above. An empty
// port set (removing the group) only needs a valid group. The error matches ErrInvalid.
func (l LAG) Validate() error {
	if err := l.ID.Validate(); err != nil {
		return err
	}
	if l.Ports.IsEmpty() {
		return nil
	}
	if l.Ports.Len() < LAGMinPorts || l.Ports.Len() > LAGPortsPerGroup {
		return invalidf("escp: a LAG takes %d to %d ports, got %d", LAGMinPorts, LAGPortsPerGroup, l.Ports.Len())
	}
	first, last := l.ID.Range()
	for _, p := range l.Ports.Ports() {
		if p < first || p > last {
			return invalidf("escp: port %d cannot be in LAG %d (LAG %d takes ports %d-%d)", p, l.ID, l.ID, first, last)
		}
	}
	return nil
}

// LAG is one link aggregation group (Switching > Port Trunk).
type LAG struct {
	ID    LAGID
	Ports PortSet // empty when the group is unused
}
