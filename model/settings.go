package model

// Setting is one page of the switch's web UI as the library covers it: the methods that read it, the
// methods that write it with the class of each write, and the types they use. It is the one place the
// list of what the library can read and write is kept: the writers take their Impact from it, and the
// settings table of the README and the lists of the guides are generated from it (`make settings-docs`;
// a test fails when they are stale, or when a method of the client is missing here).
type Setting struct {
	// Page is the web UI page ("Ports (Port Setting)").
	Page string
	// Reads are the Client methods that read the page.
	Reads []Method
	// Writes are the Client methods that write it, with their class. Empty for a page
	// that is read only.
	Writes []WriteMethod
	// WriteNote says why Writes is empty, or what writes it instead ("read only", "through `SetVLANMode`").
	WriteNote string
	// Types are the exported types the methods take and return, for the reader of the documentation.
	Types []string
}

// WriteMethod is a Client method that changes the switch, and the Impact it declares to a write gate.
type WriteMethod struct {
	Name   Method
	Impact Impact
}

// settings is the table. A class is a decision of what kind of change a method is (see Impact): change
// one here, and every writer, every guide and the tests follow.
var settings = []Setting{
	{Page: "Device (System Info)", Reads: []Method{MethodInfo, MethodSystem},
		Writes: []WriteMethod{{MethodSetHostname, ImpactCosmetic}, {MethodSetIPConfig, ImpactManagement}, {MethodEnableDHCP, ImpactManagement}},
		Types:  []string{"Info", "System", "IPConfig", "Hostname"}},
	{Page: "LED", Reads: []Method{MethodLED}, Writes: []WriteMethod{{MethodSetLED, ImpactCosmetic}}, Types: []string{"bool"}},
	{Page: "Ports (Port Setting)", Reads: []Method{MethodPorts}, Writes: []WriteMethod{{MethodSetPort, ImpactManagement}},
		Types: []string{"Port", "PortConfig", "Speed"}},
	{Page: "Port statistics", Reads: []Method{MethodPortStats}, WriteNote: "read only", Types: []string{"PortStats"}},
	{Page: "Link aggregation (Port Trunk)", Reads: []Method{MethodLAGs}, Writes: []WriteMethod{{MethodSetLAG, ImpactManagement}}, Types: []string{"LAG"}},
	{Page: "Port mirror", Reads: []Method{MethodPortMirror}, Writes: []WriteMethod{{MethodSetPortMirror, ImpactManagement}}, Types: []string{"Mirror"}},
	{Page: "Bandwidth control", Reads: []Method{MethodBandwidth}, Writes: []WriteMethod{{MethodSetBandwidth, ImpactTraffic}}, Types: []string{"PortBandwidth"}},
	{Page: "Storm control", Reads: []Method{MethodStormControl}, Writes: []WriteMethod{{MethodSetStormControl, ImpactTraffic}},
		Types: []string{"PortStorm", "StormConfig"}},
	{Page: "QoS Basic", Reads: []Method{MethodQoS}, Writes: []WriteMethod{{MethodSetQoSMode, ImpactTraffic}, {MethodSetPortPriorities, ImpactTraffic}},
		Types: []string{"QoS", "QoSMode", "Priority"}},
	{Page: "IGMP snooping", Reads: []Method{MethodIGMP}, Writes: []WriteMethod{{MethodSetIGMP, ImpactTraffic}},
		Types: []string{"IGMP", "IGMPConfig", "IGMPGroup"}},
	{Page: "Loop prevention", Reads: []Method{MethodLoopPrevention}, Writes: []WriteMethod{{MethodSetLoopPrevention, ImpactManagement}}, Types: []string{"bool"}},
	{Page: "VLAN mode", Reads: []Method{MethodVLANMode}, Writes: []WriteMethod{{MethodSetVLANMode, ImpactManagement}},
		Types: []string{"VLANMode", "VLANModeSetting"}},
	{Page: "MTU VLAN", Reads: []Method{MethodMTUVLAN}, WriteNote: "through `SetVLANMode`", Types: []string{"MTUVLAN"}},
	{Page: "Port-based VLAN", Reads: []Method{MethodPortBasedVLAN}, Writes: []WriteMethod{{MethodSetPortBasedVLAN, ImpactManagement}},
		Types: []string{"PortBasedVLAN", "PortVLAN"}},
	{Page: "802.1Q VLAN and PVID", Reads: []Method{MethodDot1Q},
		Writes: []WriteMethod{{MethodSetDot1QVLAN, ImpactManagement}, {MethodDeleteDot1QVLAN, ImpactManagement}, {MethodSetPVIDs, ImpactManagement}},
		Types:  []string{"Dot1Q", "Dot1QVLAN", "PortPVID"}},
	{Page: "Configuration file", Reads: []Method{MethodConfigBackup}, WriteNote: "restore is not offered", Types: []string{"ConfigFile"}},
}

// Settings returns the table of what the client reads and writes. The result is a copy.
func Settings() []Setting {
	out := make([]Setting, len(settings))
	for i, s := range settings {
		s.Reads = append([]Method(nil), s.Reads...)
		s.Writes = append([]WriteMethod(nil), s.Writes...)
		s.Types = append([]string(nil), s.Types...)
		out[i] = s
	}
	return out
}

// impactOf is the class the table gives a write method. A method that is not in the table has no
// class (the zero Impact), which every gate refuses: a writer that was added without a row fails
// closed, and a test names it.
func impactOf(method Method) Impact {
	for _, s := range settings {
		for _, w := range s.Writes {
			if w.Name == method {
				return w.Impact
			}
		}
	}
	return 0
}
