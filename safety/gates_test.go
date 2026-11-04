package safety

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/davidlukac/escp-go/model"
)

var errFirst, errSecond = errors.New("first gate"), errors.New("second gate")

func allow(calls *[]string, name string) model.WriteGate {
	return func(context.Context, model.Reader, model.Operation) error {
		*calls = append(*calls, name)
		return nil
	}
}

func refuse(calls *[]string, name string, err error) model.WriteGate {
	return func(context.Context, model.Reader, model.Operation) error {
		*calls = append(*calls, name)
		return err
	}
}

func TestChain(t *testing.T) {
	ctx, op := context.Background(), model.Operation{Name: model.MethodSetLED, Impact: model.ImpactCosmetic}
	t.Run("nothing, or only nil gates, gates nothing", func(t *testing.T) {
		if Chain() != nil || Chain(nil, nil) != nil {
			t.Fatal("a chain of no gates must be nil, which the client reads as no gate")
		}
	})
	t.Run("one gate is that gate", func(t *testing.T) {
		var calls []string
		g := Chain(nil, refuse(&calls, "a", errFirst), nil)
		if err := g(ctx, nil, op); !errors.Is(err, errFirst) || !reflect.DeepEqual(calls, []string{"a"}) {
			t.Fatalf("err=%v calls=%v", err, calls)
		}
	})
	t.Run("all allow: all are asked, in order", func(t *testing.T) {
		var calls []string
		g := Chain(allow(&calls, "a"), nil, allow(&calls, "b"), allow(&calls, "c"))
		if err := g(ctx, nil, op); err != nil || !reflect.DeepEqual(calls, []string{"a", "b", "c"}) {
			t.Fatalf("err=%v calls=%v", err, calls)
		}
	})
	t.Run("the first refusal wins and the later gates are not asked", func(t *testing.T) {
		var calls []string
		g := Chain(allow(&calls, "a"), refuse(&calls, "b", errFirst), refuse(&calls, "c", errSecond))
		if err := g(ctx, nil, op); !errors.Is(err, errFirst) || !reflect.DeepEqual(calls, []string{"a", "b"}) {
			t.Fatalf("err=%v calls=%v", err, calls)
		}
	})
	t.Run("the operation reaches every gate unchanged", func(t *testing.T) {
		var seen []model.Operation
		spy := func(_ context.Context, _ model.Reader, o model.Operation) error { seen = append(seen, o); return nil }
		want := model.Operation{Name: model.MethodSetLAG, Ports: model.MustPortSet(5, 6), Impact: model.ImpactManagement}
		if err := Chain(spy, spy)(ctx, nil, want); err != nil || len(seen) != 2 || !reflect.DeepEqual(seen[0], want) || !reflect.DeepEqual(seen[1], want) {
			t.Fatalf("err=%v seen=%+v", err, seen)
		}
	})
}

func TestMaxImpact(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name   string
		max    model.Impact
		impact model.Impact
		refuse bool
	}{
		{"cosmetic under cosmetic", model.ImpactCosmetic, model.ImpactCosmetic, false},
		{"traffic over cosmetic", model.ImpactCosmetic, model.ImpactTraffic, true},
		{"management over cosmetic", model.ImpactCosmetic, model.ImpactManagement, true},
		{"cosmetic under traffic", model.ImpactTraffic, model.ImpactCosmetic, false},
		{"traffic under traffic", model.ImpactTraffic, model.ImpactTraffic, false},
		{"management over traffic", model.ImpactTraffic, model.ImpactManagement, true},
		{"management under management", model.ImpactManagement, model.ImpactManagement, false},
		{"an operation that declares no impact is refused", model.ImpactManagement, 0, true},
		{"an operation with an impossible impact is refused", model.ImpactManagement, model.Impact(9), true},
		{"a max that is no class refuses everything", 0, model.ImpactCosmetic, true},
		{"a max above the classes refuses everything", model.Impact(4), model.ImpactCosmetic, true},
		{"a max far above the classes refuses everything", model.Impact(99), model.ImpactManagement, true},
		{"a negative max refuses everything", model.Impact(-1), model.ImpactCosmetic, true},
		{"an impossible impact is refused even under a max above the classes", model.Impact(99), model.Impact(50), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			op := model.Operation{Name: "SetX", Impact: tc.impact}
			err := MaxImpact(tc.max)(ctx, nil, op)
			var got *ImpactError
			switch {
			case !tc.refuse && err != nil:
				t.Fatalf("want it allowed, got %v", err)
			case tc.refuse && !errors.As(err, &got):
				t.Fatalf("want an *ImpactError, got %v", err)
			case tc.refuse && (got.Op.Name != "SetX" || got.Max != tc.max):
				t.Fatalf("the refusal must carry the operation and the limit: %+v", got)
			}
		})
	}
	t.Run("message", func(t *testing.T) {
		err := MaxImpact(model.ImpactTraffic)(ctx, nil, model.Operation{Name: model.MethodSetLAG, Impact: model.ImpactManagement})
		if want := "SetLAG has impact management, above the allowed traffic"; err == nil || err.Error() != want {
			t.Fatalf("got %v, want %q", err, want)
		}
	})
}

func TestDryRun(t *testing.T) {
	ctx := context.Background()
	d := NewDryRun()
	if len(d.Operations()) != 0 {
		t.Fatal("a new dry run has recorded nothing")
	}
	gate := d.Gate()
	a := model.Operation{Name: model.MethodSetPort, Ports: model.MustPortSet(3), Impact: model.ImpactTraffic}
	b := model.Operation{Name: model.MethodSetLED, Impact: model.ImpactCosmetic}
	for _, op := range []model.Operation{a, b} {
		t.Run(fmt.Sprint(op), func(t *testing.T) {
			if err := gate(ctx, nil, op); !errors.Is(err, ErrDryRun) {
				t.Fatalf("a dry run refuses every write with ErrDryRun, got %v", err)
			}
		})
	}
	got := d.Operations()
	if !reflect.DeepEqual(got, []model.Operation{a, b}) {
		t.Fatalf("recorded %+v, want %+v in order", got, []model.Operation{a, b})
	}
	// what is handed out is a copy
	got[0].Ports = model.MustPortSet(8)
	if again := d.Operations(); again[0].Ports != model.MustPortSet(3) {
		t.Fatalf("the record changed through a value it does not own: %+v", again[0])
	}
}

// Gates are composed as the front end wishes; a refusal by an earlier gate keeps the dry run empty.
func TestDryRunRecordsOnlyWhatTheEarlierGatesAllowed(t *testing.T) {
	d := NewDryRun()
	gate := Chain(MaxImpact(model.ImpactTraffic), d.Gate())
	ctx := context.Background()
	var tooMuch *ImpactError
	if err := gate(ctx, nil, model.Operation{Name: model.MethodSetLAG, Impact: model.ImpactManagement}); !errors.As(err, &tooMuch) {
		t.Fatalf("got %v", err)
	}
	if err := gate(ctx, nil, model.Operation{Name: model.MethodSetPort, Impact: model.ImpactTraffic}); !errors.Is(err, ErrDryRun) {
		t.Fatalf("got %v", err)
	}
	if ops := d.Operations(); len(ops) != 1 || ops[0].Name != model.MethodSetPort {
		t.Fatalf("recorded %+v, want only SetPort", ops)
	}
}

// The firmware gate judges the firmware, whatever the operation is.
func TestFirmwareGateIgnoresTheOperation(t *testing.T) {
	for _, impact := range []model.Impact{model.ImpactCosmetic, model.ImpactTraffic, model.ImpactManagement, 0} {
		t.Run(fmt.Sprint(impact), func(t *testing.T) {
			op := model.Operation{Name: "SetX", Impact: impact}
			if err := FirmwareGate()(context.Background(), &infoSource{firmware: "1.0.0 Build 20250710 Rel.71093"}, op); err != nil {
				t.Errorf("verified build, impact %v: %v", impact, err)
			}
			var fe *FirmwareError
			if err := FirmwareGate()(context.Background(), &infoSource{firmware: "1.0.0 Build 20990101 Rel.1"}, op); !errors.As(err, &fe) {
				t.Errorf("unverified build, impact %v: %v", impact, err)
			}
		})
	}
}
