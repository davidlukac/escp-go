package model

import "slices"

// Method is the name of a Client method: the identity of a read or a write in the settings table,
// in the Operation a WriteGate is handed, and in the log. It is the text of the method's own name,
// so it reads the same in a log line, a JSON file and a dry-run report. The constants below are the
// only spelling of those names in the library; a name that is no constant is no method (Validate).
type Method string

// The reads, as listed by Settings.
const (
	MethodInfo           Method = "Info"
	MethodSystem         Method = "System"
	MethodLED            Method = "LED"
	MethodPorts          Method = "Ports"
	MethodPortStats      Method = "PortStats"
	MethodLAGs           Method = "LAGs"
	MethodPortMirror     Method = "PortMirror"
	MethodBandwidth      Method = "Bandwidth"
	MethodStormControl   Method = "StormControl"
	MethodQoS            Method = "QoS"
	MethodIGMP           Method = "IGMP"
	MethodLoopPrevention Method = "LoopPrevention"
	MethodVLANMode       Method = "VLANMode"
	MethodMTUVLAN        Method = "MTUVLAN"
	MethodPortBasedVLAN  Method = "PortBasedVLAN"
	MethodDot1Q          Method = "Dot1Q"
	MethodConfigBackup   Method = "ConfigBackup"
)

// The writes, as listed by Settings, with the Impact each declares to a WriteGate.
const (
	MethodSetHostname       Method = "SetHostname"
	MethodSetIPConfig       Method = "SetIPConfig"
	MethodEnableDHCP        Method = "EnableDHCP"
	MethodSetLED            Method = "SetLED"
	MethodSetPort           Method = "SetPort"
	MethodSetLAG            Method = "SetLAG"
	MethodSetPortMirror     Method = "SetPortMirror"
	MethodSetBandwidth      Method = "SetBandwidth"
	MethodSetStormControl   Method = "SetStormControl"
	MethodSetQoSMode        Method = "SetQoSMode"
	MethodSetPortPriorities Method = "SetPortPriorities"
	MethodSetIGMP           Method = "SetIGMP"
	MethodSetLoopPrevention Method = "SetLoopPrevention"
	MethodSetVLANMode       Method = "SetVLANMode"
	MethodSetPortBasedVLAN  Method = "SetPortBasedVLAN"
	MethodSetDot1QVLAN      Method = "SetDot1QVLAN"
	MethodDeleteDot1QVLAN   Method = "DeleteDot1QVLAN"
	MethodSetPVIDs          Method = "SetPVIDs"
)

// String is the method's name.
func (m Method) String() string { return string(m) }

// Validate reports whether m is a method of the settings table, a read or a write; the error
// matches ErrInvalid.
func (m Method) Validate() error {
	for _, s := range settings {
		if slices.Contains(s.Reads, m) {
			return nil
		}
		for _, w := range s.Writes {
			if w.Name == m {
				return nil
			}
		}
	}
	return invalidf("escp: %q is not a method of the client", string(m))
}

// ParseMethod converts a method name, as String spells it; any other text is an error that matches
// ErrInvalid.
func ParseMethod(name string) (Method, error) {
	m := Method(name)
	return m, m.Validate()
}

// MarshalText is the name of the method; a value that is no method is an error, since a name that
// no decoder reads back would lose it.
func (m Method) MarshalText() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return []byte(m), nil
}

// UnmarshalText reads a method name with the rule of ParseMethod.
func (m *Method) UnmarshalText(text []byte) error {
	v, err := ParseMethod(string(text))
	if err != nil {
		return err
	}
	*m = v
	return nil
}

// Operation is the Operation of write m: its name and the Impact the settings table gives it. A
// method that is not a write in the table gets no class (the zero Impact), which every gate refuses.
func (m Method) Operation() Operation { return Operation{Name: m, Impact: impactOf(m)} }

// On returns o naming the ports of a write.
func (o Operation) On(ports PortSet) Operation {
	o.Ports = ports
	return o
}

// Replacing returns o replacing or removing group g.
func (o Operation) Replacing(g Group) Operation {
	o.Replaces = g
	return o
}
