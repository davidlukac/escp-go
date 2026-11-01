package safety_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/davidlukac/escp-go"
	"github.com/davidlukac/escp-go/safety"
)

// firmware is an escp.Reader that reports a fixed firmware version. A gate is handed the client in
// real use; a test hands it a double that implements the reads the gate makes and embeds the
// interface for the rest (a read the gate was not expected to make panics).
func firmware(version string) escp.Reader { return reportedFirmware{version: version} }

type reportedFirmware struct {
	escp.Reader
	version string
}

func (f reportedFirmware) Info(context.Context) (escp.Info, error) {
	return escp.Info{Firmware: escp.ParseFirmware(f.version)}, nil
}

// A gate belongs to one switch: it remembers a verified build, so each switch gets its own.
func ExampleFirmwareGate() {
	err := safety.FirmwareGate()(context.Background(), firmware("1.0.0 Build 20230218 Rel.50633"), escp.Operation{})
	fmt.Println("verified build:", err)

	err = safety.FirmwareGate()(context.Background(), firmware("1.0.0 Build 20990101 Rel.1"), escp.Operation{})
	var refused *safety.FirmwareError
	if errors.As(err, &refused) && refused.Reason == safety.FirmwareUnverified {
		fmt.Println("refused build", refused.Firmware.Build, "- verified:", refused.Verified)
	}
	// Output:
	// verified build: <nil>
	// refused build 20990101 - verified: [20230218 20250710]
}

func ExampleCheckFirmware() {
	fmt.Println(safety.CheckFirmware(escp.ParseFirmware("1.0.0 Build 20250710 Rel.71093")))
	err := safety.CheckFirmware(escp.ParseFirmware("custom"))
	var fe *safety.FirmwareError
	fmt.Println(errors.As(err, &fe) && fe.Reason == safety.FirmwareNoBuildDate)
	// Output:
	// <nil>
	// true
}

// UseOf states facts about a port; the caller decides what they mean for its write.
func ExampleUseOf() {
	ports := []escp.Port{
		{ID: 1, ActualSpeed: escp.Speed1000Full, LAG: 1},
		{ID: 7, ActualSpeed: escp.SpeedLinkDown},
	}
	for _, id := range []escp.PortID{1, 7, 9} {
		u := safety.UseOf(ports, id)
		fmt.Printf("port %d: exists=%v link=%v lag=%v\n", id, u.Exists, u.LinkUp(), u.InLAG())
	}
	// Output:
	// port 1: exists=true link=true lag=true
	// port 7: exists=true link=false lag=false
	// port 9: exists=false link=false lag=false
}

// CheckPorts names the lowest-numbered port a write should not touch; the caller words the refusal.
func ExampleCheckPorts() {
	ports := []escp.Port{
		{ID: 1, ActualSpeed: escp.Speed1000Full, LAG: 1},
		{ID: 2, ActualSpeed: escp.SpeedLinkDown, LAG: 1},
		{ID: 7, ActualSpeed: escp.SpeedLinkDown},
	}
	fmt.Println(safety.CheckPorts(ports, safety.PortRule{}, escp.MustPortSet(7)))
	err := safety.CheckPorts(ports, safety.PortRule{}, escp.MustPortSet(7, 2))
	var used *safety.PortUseError
	if errors.As(err, &used) && used.Reason == safety.PortInLAG {
		fmt.Printf("port %d is in LAG %d\n", used.Port, used.LAG)
	}
	// Editing LAG 1 itself tolerates its idle members, but never a link.
	fmt.Println(safety.CheckPorts(ports, safety.PortRule{TolerateLAG: 1}, escp.MustPortSet(2)))
	fmt.Println(safety.CheckPorts(ports, safety.PortRule{TolerateLAG: 1}, escp.MustPortSet(1)))
	// Output:
	// <nil>
	// port 2 is in LAG 1
	// <nil>
	// port 1 has a link up (1000Mbps-full)
}

// A dry run records what a front end would write. The gates are chained so that the dry run only
// sees what the others allowed; the client returns ErrDryRun for every write and sends nothing.
func ExampleDryRun() {
	dry := safety.NewDryRun()
	gate := safety.Chain(safety.MaxImpact(escp.ImpactTraffic), dry.Gate())
	ctx := context.Background()

	for _, op := range []escp.Operation{
		{Name: "SetLED", Impact: escp.ImpactCosmetic},
		{Name: "SetBandwidth", Ports: escp.MustPortSet(7), Impact: escp.ImpactTraffic},
		{Name: "SetLAG", Ports: escp.MustPortSet(5, 6), Impact: escp.ImpactManagement},
	} {
		err := gate(ctx, firmware(""), op)
		var tooMuch *safety.ImpactError
		switch {
		case errors.Is(err, safety.ErrDryRun):
			fmt.Println("would write:", op.Name)
		case errors.As(err, &tooMuch):
			fmt.Println("refused:", tooMuch)
		}
	}
	fmt.Println(len(dry.Operations()), "recorded")
	// Output:
	// would write: SetLED
	// would write: SetBandwidth
	// refused: SetLAG has impact management, above the allowed traffic
	// 2 recorded
}

// switchPorts is a switch with these ports, no link aggregation and no VLANs: the reads PortGate
// makes, which a Client has. Any other read panics.
func switchPorts(ports ...escp.Port) escp.Reader { return portsOnly{ports: ports} }

type portsOnly struct {
	escp.Reader
	ports []escp.Port
}

func (p portsOnly) Ports(context.Context) ([]escp.Port, error) { return p.ports, nil }

func (portsOnly) LAGs(context.Context) ([]escp.LAG, error) { return nil, nil }

func (portsOnly) PortBasedVLAN(context.Context) (escp.PortBasedVLAN, error) {
	return escp.PortBasedVLAN{}, nil
}

func (portsOnly) Dot1Q(context.Context) (escp.Dot1Q, error) { return escp.Dot1Q{}, nil }

// MaxImpact refuses a write whose worst case is above a ceiling, whatever its arguments. A front
// end that must never cut the management path allows nothing above traffic.
func ExampleMaxImpact() {
	gate := safety.MaxImpact(escp.ImpactTraffic)
	ctx := context.Background()
	for _, op := range []escp.Operation{
		{Name: "SetLED", Impact: escp.ImpactCosmetic},
		{Name: "SetBandwidth", Impact: escp.ImpactTraffic},
		{Name: "SetPort", Impact: escp.ImpactManagement},
		{Name: "SetIPConfig", Impact: escp.ImpactManagement},
	} {
		fmt.Printf("%s: %v\n", op.Name, gate(ctx, firmware(""), op))
	}
	// Output:
	// SetLED: <nil>
	// SetBandwidth: <nil>
	// SetPort: SetPort has impact management, above the allowed traffic
	// SetIPConfig: SetIPConfig has impact management, above the allowed traffic
}

// PortGate refuses a write that touches a port with a link or in a link aggregation group. It asks
// the switch (a Client lists its ports) at the moment of the write.
func ExamplePortGate() {
	sw := switchPorts(
		escp.Port{ID: 1, ActualSpeed: escp.Speed1000Full},
		escp.Port{ID: 2, ActualSpeed: escp.SpeedLinkDown},
	)
	gate := safety.PortGate()
	ctx := context.Background()
	fmt.Println("port 2:", gate(ctx, sw, escp.Operation{Name: "SetPort", Ports: escp.MustPortSet(2)}))
	fmt.Println("port 1:", gate(ctx, sw, escp.Operation{Name: "SetPort", Ports: escp.MustPortSet(1)}))
	// Output:
	// port 2: <nil>
	// port 1: port 1 has a link up (1000Mbps-full)
}

// Chain runs its gates in order and stops at the first refusal, so the cheap checks go first. A
// gate that needs the switch (PortGate) is only asked about writes that passed the others.
func ExampleChain() {
	sw := switchPorts(escp.Port{ID: 1, ActualSpeed: escp.Speed1000Full}, escp.Port{ID: 2, ActualSpeed: escp.SpeedLinkDown})
	gate := safety.Chain(safety.MaxImpact(escp.ImpactTraffic), safety.PortGate())
	ctx := context.Background()
	fmt.Println(gate(ctx, sw, escp.Operation{Name: "SetBandwidth", Ports: escp.MustPortSet(2), Impact: escp.ImpactTraffic}))
	fmt.Println(gate(ctx, sw, escp.Operation{Name: "SetBandwidth", Ports: escp.MustPortSet(1), Impact: escp.ImpactTraffic}))
	fmt.Println(gate(ctx, sw, escp.Operation{Name: "SetLAG", Ports: escp.MustPortSet(2), Impact: escp.ImpactManagement}))
	// Output:
	// <nil>
	// port 1 has a link up (1000Mbps-full)
	// SetLAG has impact management, above the allowed traffic
}

// The compatibility table says which switches and firmware builds the library was verified on;
// FirmwareGate allows writes only on the verified ones.
func ExampleCompatibilityRows() {
	for _, row := range safety.CompatibilityRows()[:2] {
		fmt.Printf("%s %s, firmware %s: %s\n", row.Model, row.Hardware, row.Firmware, row.Support)
	}
	// Output:
	// TL-SG108E v6, firmware 1.0.0 Build 20230218: verified
	// TL-SG108E v6, firmware 1.0.0 Build 20250710: verified
}
