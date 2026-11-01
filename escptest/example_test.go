package escptest_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/davidlukac/escp-go"
	"github.com/davidlukac/escp-go/escptest"
)

// dial aims a client at the fake. A real switch always uses UDP ports 29808 and 29809, so the
// client is told the fake's port, a free local port to receive on, and loopback as the broadcast
// address; everything else is as for a real switch.
func dial(sw *escptest.Switch) *escp.Client {
	hostPort, err := escptest.FreePort()
	if err != nil {
		panic(err)
	}
	c, err := escp.Dial(escp.Config{
		Host: "127.0.0.1", SwitchPort: sw.UDPPort(), HostPort: hostPort, BroadcastAddr: escptest.Loopback(),
		Username: escptest.Username, Password: escptest.Password,
	})
	if err != nil {
		panic(err)
	}
	return c
}

// In a test, escptest.Start(t) does the same and stops the switch when the test ends. Listen is
// for code that has no testing.T, like this example.
func ExampleListen() {
	sw, err := escptest.Listen()
	if err != nil {
		panic(err)
	}
	defer func() { _ = sw.Close() }()
	c := dial(sw)
	defer func() { _ = c.Close() }()

	ctx := context.Background()
	on, _ := c.LED(ctx)
	_ = c.SetLED(ctx, !on)
	off, _ := c.LED(ctx)
	fmt.Println("LED was", on, "now", off, "- writes seen by the switch:", sw.WriteCount())
	// Output: LED was true now false - writes seen by the switch: 1
}

// EnableModel makes the switch enforce the rules a real TL-SG108E enforces between settings, with
// the refusal codes measured on hardware. Here a port that a mirror copies cannot join a link
// aggregation group: the switch refuses the group, and the client reports it as a *RejectedError.
func ExampleSwitch_EnableModel() {
	sw, err := escptest.Listen()
	if err != nil {
		panic(err)
	}
	defer func() { _ = sw.Close() }()
	sw.EnableModel(map[int]escptest.LinkSpeed{1: escptest.Link1000Full})
	c := dial(sw)
	defer func() { _ = c.Close() }()

	ctx := context.Background()
	fmt.Println("mirror:", c.SetPortMirror(ctx, escp.Mirror{Enabled: true, Destination: 4, IngressPorts: escp.MustPortSet(2)}))
	err = c.SetLAG(ctx, escp.LAG{ID: 1, Ports: escp.MustPortSet(2, 3)})
	var refused *escp.RejectedError
	if errors.As(err, &refused) {
		fmt.Println("LAG refused with code", refused.Code)
	}
	// Output:
	// mirror: <nil>
	// LAG refused with code 7206
}

// Faults are set inside Configure, which holds the switch's lock. This switch rejects every write
// from the second one on, with a code the client reports as a *RejectedError; WriteCount shows
// that only the first write was accepted.
func ExampleSwitch_Configure() {
	sw, err := escptest.Listen()
	if err != nil {
		panic(err)
	}
	defer func() { _ = sw.Close() }()
	sw.Configure(func(f *escptest.Faults) {
		f.RejectSetsFrom = 2
	})
	c := dial(sw)
	defer func() { _ = c.Close() }()

	ctx := context.Background()
	fmt.Println("first write:", c.SetLED(ctx, false))
	err = c.SetLED(ctx, true)
	fmt.Println("second write rejected:", errors.Is(err, escp.ErrRejected))
	fmt.Println("accepted by the switch:", sw.WriteCount())
	// Output:
	// first write: <nil>
	// second write rejected: true
	// accepted by the switch: 1
}
