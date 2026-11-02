package escp

// Setting is one page of the switch's web UI as the library covers it: the methods that read it, the
// methods that write it with the class of each write, and the types they use. It is the one place the
// list of what the library can read and write is kept: the writers take their Impact from it, and the
// settings table of the README and the lists of the guides are generated from it (`make settings-docs`;
// a test fails when they are stale, or when a method of the client is missing here).
type Setting struct {
	// Page is the web UI page ("Ports (Port Setting)").
	Page string
	// Reads are the names of the Client methods that read the page.
	Reads []string
	// Writes are the names of the Client methods that write it, with their class. Empty for a page
	// that is read only.
	Writes []WriteMethod
	// WriteNote says why Writes is empty, or what writes it instead ("read only", "through `SetVLANMode`").
	WriteNote string
	// Types are the exported types the methods take and return, for the reader of the documentation.
	Types []string
}

// WriteMethod is a Client method that changes the switch, and the Impact it declares to a write gate.
type WriteMethod struct {
	Name   string
	Impact Impact
}

// settings is the table. A class is a decision of what kind of change a method is (see Impact): change
// one here, and every writer, every guide and the tests follow.
var settings = []Setting{
	{Page: "Device (System Info)", Reads: []string{"Info", "System"},
		Writes: []WriteMethod{{"SetHostname", ImpactCosmetic}, {"SetIPConfig", ImpactManagement}, {"EnableDHCP", ImpactManagement}},
		Types:  []string{"Info", "System", "IPConfig", "Hostname"}},
	{Page: "LED", Reads: []string{"LED"}, Writes: []WriteMethod{{"SetLED", ImpactCosmetic}}, Types: []string{"bool"}},
	{Page: "Ports (Port Setting)", Reads: []string{"Ports"}, Writes: []WriteMethod{{"SetPort", ImpactManagement}},
		Types: []string{"Port", "PortConfig", "Speed"}},
	{Page: "Port statistics", Reads: []string{"PortStats"}, WriteNote: "read only", Types: []string{"PortStats"}},
	{Page: "Link aggregation (Port Trunk)", Reads: []string{"LAGs"}, Writes: []WriteMethod{{"SetLAG", ImpactManagement}}, Types: []string{"LAG"}},
	{Page: "Port mirror", Reads: []string{"PortMirror"}, Writes: []WriteMethod{{"SetPortMirror", ImpactManagement}}, Types: []string{"Mirror"}},
	{Page: "Bandwidth control", Reads: []string{"Bandwidth"}, Writes: []WriteMethod{{"SetBandwidth", ImpactTraffic}}, Types: []string{"PortBandwidth"}},
	{Page: "Storm control", Reads: []string{"StormControl"}, Writes: []WriteMethod{{"SetStormControl", ImpactTraffic}},
		Types: []string{"PortStorm", "StormConfig"}},
	{Page: "QoS Basic", Reads: []string{"QoS"}, Writes: []WriteMethod{{"SetQoSMode", ImpactTraffic}, {"SetPortPriorities", ImpactTraffic}},
		Types: []string{"QoS", "QoSMode", "Priority"}},
	{Page: "IGMP snooping", Reads: []string{"IGMP"}, Writes: []WriteMethod{{"SetIGMP", ImpactTraffic}},
		Types: []string{"IGMP", "IGMPConfig", "IGMPGroup"}},
	{Page: "Loop prevention", Reads: []string{"LoopPrevention"}, Writes: []WriteMethod{{"SetLoopPrevention", ImpactManagement}}, Types: []string{"bool"}},
	{Page: "VLAN mode", Reads: []string{"VLANMode"}, Writes: []WriteMethod{{"SetVLANMode", ImpactManagement}},
		Types: []string{"VLANMode", "VLANModeSetting"}},
	{Page: "MTU VLAN", Reads: []string{"MTUVLAN"}, WriteNote: "through `SetVLANMode`", Types: []string{"MTUVLAN"}},
	{Page: "Port-based VLAN", Reads: []string{"PortBasedVLAN"}, Writes: []WriteMethod{{"SetPortBasedVLAN", ImpactManagement}},
		Types: []string{"PortBasedVLAN", "PortVLAN"}},
	{Page: "802.1Q VLAN and PVID", Reads: []string{"Dot1Q"},
		Writes: []WriteMethod{{"SetDot1QVLAN", ImpactManagement}, {"DeleteDot1QVLAN", ImpactManagement}, {"SetPVIDs", ImpactManagement}},
		Types:  []string{"Dot1Q", "Dot1QVLAN", "PortPVID"}},
	{Page: "Configuration file", Reads: []string{"ConfigBackup"}, WriteNote: "restore is not offered", Types: []string{"ConfigFile"}},
}

// Settings returns the table of what the client reads and writes. The result is a copy.
func Settings() []Setting {
	out := make([]Setting, len(settings))
	for i, s := range settings {
		s.Reads = append([]string(nil), s.Reads...)
		s.Writes = append([]WriteMethod(nil), s.Writes...)
		s.Types = append([]string(nil), s.Types...)
		out[i] = s
	}
	return out
}

// impactOf is the class the table gives a write method. A method that is not in the table has no
// class (the zero Impact), which every gate refuses: a writer that was added without a row fails
// closed, and a test names it.
func impactOf(method string) Impact {
	for _, s := range settings {
		for _, w := range s.Writes {
			if w.Name == method {
				return w.Impact
			}
		}
	}
	return 0
}
