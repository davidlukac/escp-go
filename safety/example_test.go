package safety_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/davidlukac/escp-go/model"
	"github.com/davidlukac/escp-go/safety"
)

// firmware is an escp.Reader that reports a fixed firmware version. A gate is handed the client in
// real use; a test hands it a double that implements the reads the gate makes and embeds the
// interface for the rest (a read the gate was not expected to make panics).
func firmware(version string) model.Reader { return reportedFirmware{version: version} }

type reportedFirmware struct {
	model.Reader
	version string
}

func (f reportedFirmware) Info(context.Context) (model.Info, error) {
	return model.Info{Firmware: model.ParseFirmware(f.version)}, nil
}

// SwitchMAC is zero: the double does not know its switch, so the gate checks it at every write.
func (f reportedFirmware) SwitchMAC() model.MAC { return model.MAC{} }

// A gate remembers a verified build for each switch, by its MAC address.
func ExampleFirmwareGate() {
	err := safety.FirmwareGate()(context.Background(), firmware("1.0.0 Build 20230218 Rel.50633"), model.Operation{})
	fmt.Println("verified build:", err)

	err = safety.FirmwareGate()(context.Background(), firmware("1.0.0 Build 20990101 Rel.1"), model.Operation{})
	var refused *safety.FirmwareError
	if errors.As(err, &refused) && refused.Reason == safety.FirmwareUnverified {
		fmt.Println("refused build", refused.Firmware.BuildDate(), "- verified:", refused.Verified)
	}
	// Output:
	// verified build: <nil>
	// refused build 20990101 - verified: [20230218 20250710]
}

func ExampleCheckFirmware() {
	fmt.Println(safety.CheckFirmware(model.ParseFirmware("1.0.0 Build 20250710 Rel.71093")))
	err := safety.CheckFirmware(model.ParseFirmware("custom"))
	var fe *safety.FirmwareError
	fmt.Println(errors.As(err, &fe) && fe.Reason == safety.FirmwareNoBuildDate)
	// Output:
	// <nil>
	// true
}

// UseOf states facts about a port; the caller decides what they mean for its write.
func ExampleUseOf() {
	ports := []model.Port{
		{ID: 1, ActualSpeed: model.Speed1000Full, LAG: 1},
		{ID: 7, ActualSpeed: model.SpeedLinkDown},
	}
	for _, id := range []model.PortID{1, 7, 9} {
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
	ports := []model.Port{
		{ID: 1, ActualSpeed: model.Speed1000Full, LAG: 1},
		{ID: 2, ActualSpeed: model.SpeedLinkDown, LAG: 1},
		{ID: 7, ActualSpeed: model.SpeedLinkDown},
	}
	fmt.Println(safety.CheckPorts(ports, safety.PortRule{}, model.MustPortSet(7)))
	err := safety.CheckPorts(ports, safety.PortRule{}, model.MustPortSet(7, 2))
	var used *safety.PortUseError
	if errors.As(err, &used) && used.Reason == safety.PortInLAG {
		fmt.Printf("port %d is in LAG %d\n", used.Port, used.LAG)
	}
	// Editing LAG 1 itself tolerates its idle members, but never a link.
	fmt.Println(safety.CheckPorts(ports, safety.PortRule{TolerateLAG: 1}, model.MustPortSet(2)))
	fmt.Println(safety.CheckPorts(ports, safety.PortRule{TolerateLAG: 1}, model.MustPortSet(1)))
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
	gate := safety.Chain(safety.MaxImpact(model.ImpactTraffic), dry.Gate())
	ctx := context.Background()

	for _, op := range []model.Operation{
		{Name: model.MethodSetLED, Impact: model.ImpactCosmetic},
		{Name: model.MethodSetBandwidth, Ports: model.MustPortSet(7), Impact: model.ImpactTraffic},
		{Name: model.MethodSetLAG, Ports: model.MustPortSet(5, 6), Impact: model.ImpactManagement},
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
func switchPorts(ports ...model.Port) model.Reader { return portsOnly{ports: ports} }

type portsOnly struct {
	model.Reader
	ports []model.Port
}

func (p portsOnly) Ports(context.Context) ([]model.Port, error) { return p.ports, nil }

func (portsOnly) LAGs(context.Context) ([]model.LAG, error) { return nil, nil }

func (portsOnly) PortBasedVLAN(context.Context) (model.PortBasedVLAN, error) {
	return model.PortBasedVLAN{}, nil
}

func (portsOnly) Dot1Q(context.Context) (model.Dot1Q, error) { return model.Dot1Q{}, nil }

// MaxImpact refuses a write whose worst case is above a ceiling, whatever its arguments. A front
// end that must never cut the management path allows nothing above traffic.
func ExampleMaxImpact() {
	gate := safety.MaxImpact(model.ImpactTraffic)
	ctx := context.Background()
	for _, op := range []model.Operation{
		{Name: model.MethodSetLED, Impact: model.ImpactCosmetic},
		{Name: model.MethodSetBandwidth, Impact: model.ImpactTraffic},
		{Name: model.MethodSetPort, Impact: model.ImpactManagement},
		{Name: model.MethodSetIPConfig, Impact: model.ImpactManagement},
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
		model.Port{ID: 1, ActualSpeed: model.Speed1000Full},
		model.Port{ID: 2, ActualSpeed: model.SpeedLinkDown},
	)
	gate := safety.PortGate()
	ctx := context.Background()
	fmt.Println("port 2:", gate(ctx, sw, model.Operation{Name: model.MethodSetPort, Ports: model.MustPortSet(2)}))
	fmt.Println("port 1:", gate(ctx, sw, model.Operation{Name: model.MethodSetPort, Ports: model.MustPortSet(1)}))
	// Output:
	// port 2: <nil>
	// port 1: port 1 has a link up (1000Mbps-full)
}

// Chain runs its gates in order and stops at the first refusal, so the cheap checks go first. A
// gate that needs the switch (PortGate) is only asked about writes that passed the others.
func ExampleChain() {
	sw := switchPorts(model.Port{ID: 1, ActualSpeed: model.Speed1000Full}, model.Port{ID: 2, ActualSpeed: model.SpeedLinkDown})
	gate := safety.Chain(safety.MaxImpact(model.ImpactTraffic), safety.PortGate())
	ctx := context.Background()
	fmt.Println(gate(ctx, sw, model.Operation{Name: model.MethodSetBandwidth, Ports: model.MustPortSet(2), Impact: model.ImpactTraffic}))
	fmt.Println(gate(ctx, sw, model.Operation{Name: model.MethodSetBandwidth, Ports: model.MustPortSet(1), Impact: model.ImpactTraffic}))
	fmt.Println(gate(ctx, sw, model.Operation{Name: model.MethodSetLAG, Ports: model.MustPortSet(2), Impact: model.ImpactManagement}))
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
