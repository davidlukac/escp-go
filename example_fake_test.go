package escp_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/davidlukac/escp-go"
	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/safety"
)

// The examples in this file run: they talk to the in-process fake switch of escptest, which
// models an eight-port TL-SG108E with a link on port 1 (an uplink) and nothing on the others. A
// real program dials a host or a MAC address instead (see ExampleDial); the three
// Config fields SwitchPort, HostPort and BroadcastAddr only exist to aim a client at a fake.

// Read the switch's identity. No credentials are needed: Info is the discovery reply.
func ExampleClient_Info() {
	sw, err := escptest.Listen()
	if err != nil {
		panic(err)
	}
	defer func() { _ = sw.Close() }()
	hostPort, err := escptest.FreePort()
	if err != nil {
		panic(err)
	}

	c, err := escp.Dial(escp.Config{
		Host: "127.0.0.1", SwitchPort: sw.Port(), HostPort: hostPort, BroadcastAddr: escptest.Loopback(),
		Username: escptest.Username, Password: escptest.Password,
	})
	if err != nil {
		panic(err)
	}
	defer func() { _ = c.Close() }()

	info, err := c.Info(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println(info.Model, info.Firmware)
	// Output: TL-SG108E 1.0.0 Build 20230218 Rel.50633
}

// Read the ports. A read cannot change the switch.
func ExampleClient_Ports() {
	sw, err := escptest.Listen()
	if err != nil {
		panic(err)
	}
	defer func() { _ = sw.Close() }()
	sw.EnableModel(map[int]escptest.LinkSpeed{1: escptest.Link1000Full})
	hostPort, err := escptest.FreePort()
	if err != nil {
		panic(err)
	}

	c, err := escp.Dial(escp.Config{
		Host: "127.0.0.1", SwitchPort: sw.Port(), HostPort: hostPort, BroadcastAddr: escptest.Loopback(),
		Username: escptest.Username, Password: escptest.Password,
	})
	if err != nil {
		panic(err)
	}
	defer func() { _ = c.Close() }()

	ports, err := c.Ports(context.Background())
	if err != nil {
		panic(err)
	}
	for _, p := range ports[:3] {
		fmt.Printf("port %d: %s (configured %s)\n", p.ID, p.ActualSpeed, p.ConfiguredSpeed)
	}
	// Output:
	// port 1: 1000Mbps-full (configured auto)
	// port 2: link-down (configured auto)
	// port 3: link-down (configured auto)
}

// Write behind a gate. The gate sees every write before it is sent: this one allows only
// firmware the client was verified against, nothing above a traffic-level impact, and only
// ports that have no link and belong to no link aggregation. Port 1 carries the uplink, so a
// write to it is refused and sends nothing; port 2 is free, so its write goes through.
func ExampleConfig_writeGate() {
	sw, err := escptest.Listen()
	if err != nil {
		panic(err)
	}
	defer func() { _ = sw.Close() }()
	sw.EnableModel(map[int]escptest.LinkSpeed{1: escptest.Link1000Full})
	hostPort, err := escptest.FreePort()
	if err != nil {
		panic(err)
	}

	c, err := escp.Dial(escp.Config{
		Host: "127.0.0.1", SwitchPort: sw.Port(), HostPort: hostPort, BroadcastAddr: escptest.Loopback(),
		Username: escptest.Username, Password: escptest.Password,
		WriteGate: safety.Chain(
			safety.FirmwareGate(),
			safety.MaxImpact(escp.ImpactTraffic),
			safety.PortGate(),
		),
	})
	if err != nil {
		panic(err)
	}
	defer func() { _ = c.Close() }()

	ctx := context.Background()
	err = c.SetPort(ctx, escp.PortConfig{ID: 1, Enabled: false, Speed: escp.SpeedAuto})
	var inUse *safety.PortUseError
	if errors.As(err, &inUse) {
		fmt.Println("refused:", err)
	}
	fmt.Println("writes reaching the switch:", sw.WriteCount())

	err = c.SetPort(ctx, escp.PortConfig{ID: 2, Enabled: false, Speed: escp.SpeedAuto})
	fmt.Println("port 2:", err, "- writes reaching the switch:", sw.WriteCount())
	// Output:
	// refused: port 1 has a link up (1000Mbps-full)
	// writes reaching the switch: 0
	// port 2: <nil> - writes reaching the switch: 1
}

// A set of ports is a value: no duplicates, ascending, and a port the switch cannot have is refused.
func ExampleNewPortSet() {
	set, err := escp.NewPortSet(5, 2, 3)
	fmt.Println(set, err)

	_, err = escp.NewPortSet(2, 99)
	fmt.Println(errors.Is(err, escp.ErrInvalid), err)
	// Output:
	// 2,3,5 <nil>
	// true escp: invalid port 99 (valid: 1-32)
}
