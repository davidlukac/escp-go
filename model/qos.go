package model

import (
	"fmt"
)

// QoSMode is the switch's QoS mode. The number is the wire byte; the name is the web UI's
// and the Terraform provider's spelling, which consumers keep in saved state: do not change it.
type QoSMode int

// The QoS modes the switch knows. Per-port priorities only take effect in QoSPortBased.
const (
	QoSPortBased QoSMode = 0
	QoS8021p     QoSMode = 1
	QoSDSCP8021p QoSMode = 2
)

var qosModeNames = [...]string{QoSPortBased: "port-based", QoS8021p: "802.1p", QoSDSCP8021p: "dscp-802.1p"}

// QoSModes lists the modes SetQoSMode accepts, in the order of their codes.
func QoSModes() []QoSMode { return []QoSMode{QoSPortBased, QoS8021p, QoSDSCP8021p} }

// Validate reports whether the switch knows the mode; the error matches ErrInvalid.
func (m QoSMode) Validate() error {
	if m < 0 || int(m) >= len(qosModeNames) {
		return invalidf("escp: invalid QoS mode %d (valid: 0, 1, 2)", int(m))
	}
	return nil
}

// String returns the web UI's name of the mode, or unknown(n) for a code this client does not know.
func (m QoSMode) String() string {
	if m.Validate() != nil {
		return fmt.Sprintf("unknown(%d)", int(m))
	}
	return qosModeNames[m]
}

// ParseQoSMode converts a mode name as String spells it.
func ParseQoSMode(name string) (QoSMode, error) {
	for _, m := range QoSModes() {
		if m.String() == name {
			return m, nil
		}
	}
	return 0, invalidf("escp: unknown QoS mode %q (valid: %v)", name, QoSModes())
}

// MarshalText is the web UI's name of the mode, as String spells it; a code this client does not know
// is an error, since a name that no decoder reads back would lose it.
func (m QoSMode) MarshalText() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return []byte(m.String()), nil
}

// UnmarshalText reads a mode name with the rule of ParseQoSMode.
func (m *QoSMode) UnmarshalText(text []byte) error {
	v, err := ParseQoSMode(string(text))
	if err != nil {
		return err
	}
	*m = v
	return nil
}

// Priority is a port's priority queue as the web UI shows it: PriorityLowest (1) to PriorityHighest (4).
// On the wire it is one less.
type Priority int

// The bounds of Priority.
const (
	PriorityLowest  Priority = 1
	PriorityHighest Priority = 4
)

// Validate reports whether the priority is within PriorityLowest..PriorityHighest; the error matches
// ErrInvalid.
func (p Priority) Validate() error {
	if p < PriorityLowest || p > PriorityHighest {
		return invalidf("escp: invalid priority %d (valid: %d-%d)", int(p), PriorityLowest, PriorityHighest)
	}
	return nil
}

// PortPriority is a port's priority queue (PriorityLowest to PriorityHighest).
type PortPriority struct {
	Port     PortID
	Priority Priority
}

// QoS is the QoS Basic page: the mode and, for every port, a priority. The priorities are only
// effective in QoSPortBased mode, but the switch reports them in every mode.
type QoS struct {
	Mode       QoSMode
	Priorities []PortPriority // one entry per port
}

// Validate checks the port and the priority. The error matches ErrInvalid.
func (p PortPriority) Validate() error {
	if err := p.Port.Validate(); err != nil {
		return err
	}
	if p.Priority.Validate() != nil {
		return invalidf("escp: invalid priority %d for port %d (valid: %d-%d)", p.Priority, p.Port, PriorityLowest, PriorityHighest)
	}
	return nil
}
