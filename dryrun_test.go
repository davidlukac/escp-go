package escp_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/davidlukac/escp-go"
	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
	"github.com/davidlukac/escp-go/safety"
)

// The gates of the safety package, composed as a front end would, against the real client and the
// fake switch: the firmware gate, a ceiling on the impact, and a dry run that records what would
// have been written.
//
// guarantee: safety.DryRun
func TestDryRunThroughTheRealClient(t *testing.T) {
	f := escptest.Start(t)
	f.EnableModel(nil) // the pre-write read of the other limiter needs a switch that answers
	dry := safety.NewDryRun()
	c, err := escp.Dial(escp.Config{
		Host: "127.0.0.1", Username: escptest.Username, Password: escptest.Password,
		Timeout: 300 * time.Millisecond, SwitchPort: f.UDPPort(), HostPort: escptest.FreeUDPPort(t), BroadcastAddr: escptest.Loopback(),
		WriteGate: safety.Chain(safety.FirmwareGate(), safety.MaxImpact(model.ImpactTraffic), dry.Gate()),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx := context.Background()

	// writes within the ceiling are recorded and refused with ErrDryRun, which means success here
	if err := c.SetStormControl(ctx, model.StormConfig{Port: 3, Broadcast: true, Rate: 64}); !errors.Is(err, safety.ErrDryRun) {
		t.Fatalf("SetStormControl: got %v, want ErrDryRun", err)
	}
	if err := c.SetBandwidth(ctx, model.PortBandwidth{Port: 4, Ingress: 64}); !errors.Is(err, safety.ErrDryRun) {
		t.Fatalf("SetBandwidth: got %v, want ErrDryRun", err)
	}
	if err := c.SetLED(ctx, false); !errors.Is(err, safety.ErrDryRun) {
		t.Fatalf("SetLED: got %v, want ErrDryRun", err)
	}
	// a write above the ceiling is refused before the dry run sees it: a link aggregation group, and
	// a port setting, which can turn a port off whichever it is aimed at
	var tooMuch *safety.ImpactError
	if err := c.SetLAG(ctx, model.LAG{ID: 1, Ports: model.MustPortSet(2, 3)}); !errors.As(err, &tooMuch) || tooMuch.Op.Name != model.MethodSetLAG {
		t.Fatalf("SetLAG: got %v, want an *ImpactError for SetLAG", err)
	}
	if err := c.SetPort(ctx, model.PortConfig{ID: 3, Enabled: false, Speed: model.SpeedAuto}); !errors.As(err, &tooMuch) || tooMuch.Op.Name != model.MethodSetPort {
		t.Fatalf("SetPort: got %v, want an *ImpactError for SetPort", err)
	}

	want := []model.Operation{
		{Name: model.MethodSetStormControl, Ports: model.MustPortSet(3), Impact: model.ImpactTraffic},
		{Name: model.MethodSetBandwidth, Ports: model.MustPortSet(4), Impact: model.ImpactTraffic}, // two SETs, one operation: the first refusal ends it
		{Name: model.MethodSetLED, Impact: model.ImpactCosmetic},
	}
	if got := dry.Operations(); !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded %+v, want %+v", got, want)
	}
	if n := f.WriteCount(); n != 0 {
		t.Fatalf("a dry run must send nothing, the switch received %d writes", n)
	}
}

// A write that first reads from the switch still reads, and still sends nothing.
func TestDryRunOfAWriteThatReadsFirst(t *testing.T) {
	f := escptest.Start(t)
	dry := safety.NewDryRun()
	c, err := escp.Dial(escp.Config{
		Host: "127.0.0.1", Username: escptest.Username, Password: escptest.Password,
		Timeout: 300 * time.Millisecond, SwitchPort: f.UDPPort(), HostPort: escptest.FreeUDPPort(t), BroadcastAddr: escptest.Loopback(),
		WriteGate: dry.Gate(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.SetPortPriorities(context.Background(), []model.PortPriority{{Port: 5, Priority: model.PriorityHighest}, {Port: 2, Priority: model.PriorityLowest}}); !errors.Is(err, safety.ErrDryRun) {
		t.Fatalf("got %v, want ErrDryRun", err)
	}
	want := []model.Operation{{Name: model.MethodSetPortPriorities, Ports: model.MustPortSet(2, 5), Impact: model.ImpactTraffic}}
	if got := dry.Operations(); !reflect.DeepEqual(got, want) {
		t.Fatalf("recorded %+v, want %+v", got, want)
	}
	if n := f.WriteCount(); n != 0 {
		t.Fatalf("the switch received %d writes", n)
	}
}

// PortGate through the real client: the uplink cannot be touched, named or not, and a free port can.
func TestPortGateThroughTheRealClient(t *testing.T) {
	f := escptest.Start(t)
	f.Configure(func(f *escptest.Faults) {
		f.OnGet = func(types []escptest.TLVType) ([]escptest.Setting, int32, bool) {
			var out []escptest.Setting
			for _, typ := range types {
				switch wire.TLVType(typ) {
				case wire.TLVPorts: // port 1 is the linked uplink in LAG 1, port 3 is free
					out = append(out,
						escptest.Setting{Type: escptest.TLVType(wire.TLVPorts), Value: []byte{1, 1, 1, 1, 6, 0, 0}},
						escptest.Setting{Type: escptest.TLVType(wire.TLVPorts), Value: []byte{3, 1, 0, 1, 0, 0, 0}})
				case wire.TLVLAG: // LAG 1 = ports 1 and 2
					out = append(out, escptest.Setting{Type: escptest.TLVType(wire.TLVLAG), Value: []byte{1, 0, 0, 0, 3}})
				case wire.TLVPortCount:
					out = append(out, escptest.Setting{Type: escptest.TLVType(wire.TLVPortCount), Value: []byte{8}})
				}
			}
			return out, 0, false
		}
	})
	c, err := escp.Dial(escp.Config{
		Host: "127.0.0.1", Username: escptest.Username, Password: escptest.Password,
		Timeout: 300 * time.Millisecond, SwitchPort: f.UDPPort(), HostPort: escptest.FreeUDPPort(t), BroadcastAddr: escptest.Loopback(),
		WriteGate: safety.PortGate(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx := context.Background()

	var used *safety.PortUseError
	if err := c.SetPort(ctx, model.PortConfig{ID: 1, Enabled: false, Speed: model.SpeedAuto}); !errors.As(err, &used) || used.Port != 1 || used.Reason != safety.PortLinkUp {
		t.Fatalf("disabling the uplink: got %v, want a link-up refusal for port 1", err)
	}
	// removing LAG 1 names no port, yet its member carries the link
	if err := c.SetLAG(ctx, model.LAG{ID: 1, Ports: 0}); !errors.As(err, &used) || used.Port != 1 {
		t.Fatalf("removing LAG 1: got %v, want a refusal for port 1", err)
	}
	if n := f.WriteCount(); n != 0 {
		t.Fatalf("the refusals must send nothing, the switch received %d writes", n)
	}
	if err := c.SetPort(ctx, model.PortConfig{ID: 3, Enabled: true, Speed: model.SpeedAuto}); err != nil {
		t.Fatalf("a free port: %v", err)
	}
	if n := f.WriteCount(); n != 1 {
		t.Fatalf("the free port is written once, got %d", n)
	}
}
