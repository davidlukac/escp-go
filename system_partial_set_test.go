package escp

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
)

// systemSetOf is a System-page SET built by hand (the client's writers always send all five settings): the
// device description, the DHCP flag and the address triple, each present unless its type is in omit.
func systemSetOf(omit ...wire.TLVType) []wire.TLV {
	page := []wire.TLV{
		{Type: wire.TLVHostname, Value: []byte("renamed\x00")},
		{Type: wire.TLVDHCP, Value: []byte{0}},
		{Type: wire.TLVIP, Value: []byte{127, 0, 0, 1}},
		{Type: wire.TLVNetmask, Value: []byte{255, 255, 0, 0}},
		{Type: wire.TLVGateway, Value: []byte{127, 0, 0, 2}},
	}
	var out []wire.TLV
	for _, t := range page {
		if !slices.Contains(omit, t.Type) {
			out = append(out, t)
		}
	}
	return out
}

// systemSetOp is the operation of the hand-built SETs: they go through the low-level path, as in the
// other tests that show what the switch does with a write the client would not make.
var systemSetOp = Operation{Name: "test", Impact: ImpactManagement}

// What the switch itself said, measured on firmware 20250710 (PROTOCOL.md, "The System-page SET is
// all-or-nothing"): a SET without the DHCP flag and a SET without the gateway are refused with code 3 and
// change nothing. The other omissions were not tried on a switch, so the fake refuses them by extension of
// the same rule (a SET with the device description alone could cost a real switch its address); the table
// says which rows are measured, and the fake must not accept any of them.
func TestTheFakeRefusesAPartialSystemSetAndChangesNothing(t *testing.T) {
	all := []wire.TLVType{wire.TLVHostname, wire.TLVDHCP, wire.TLVIP, wire.TLVNetmask, wire.TLVGateway}
	except := func(keep ...wire.TLVType) []wire.TLVType {
		return slices.DeleteFunc(slices.Clone(all), func(typ wire.TLVType) bool { return slices.Contains(keep, typ) })
	}
	tests := []struct {
		name string
		omit []wire.TLVType
	}{
		{"measured: without the DHCP flag", []wire.TLVType{wire.TLVDHCP}},
		{"measured: without the gateway", []wire.TLVType{wire.TLVGateway}},
		{"by extension: without the device description", []wire.TLVType{wire.TLVHostname}},
		{"by extension: without the address", []wire.TLVType{wire.TLVIP}},
		{"by extension: without the mask", []wire.TLVType{wire.TLVNetmask}},
		{"by extension: the device description alone", except(wire.TLVHostname)},
		{"by extension: the DHCP flag alone", except(wire.TLVDHCP)},
		{"by extension: the address triple alone", []wire.TLVType{wire.TLVHostname, wire.TLVDHCP}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := escptest.Start(t)
			c := dialFast(t, f, "secret")
			ctx := context.Background()
			before, err := c.Info(ctx)
			if err != nil {
				t.Fatal(err)
			}
			err = c.setTLVs(ctx, systemSetOp, systemSetOf(tt.omit...)...)
			var rejected *RejectedError
			if !errors.As(err, &rejected) || rejected.Code != 3 || !errors.Is(err, ErrRejected) {
				t.Fatalf("want a *RejectedError with code 3, got %v", err)
			}
			after, err := c.Info(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if after != before || f.Hostname() != "" || f.IPSent() != nil {
				t.Fatalf("a refused SET changed something: before %+v, after %+v, hostname %q, address %v", before, after, f.Hostname(), f.IPSent())
			}
			if n := f.WriteCount(); n != 0 {
				t.Fatalf("a refused SET must not count as accepted, got %d", n)
			}
		})
	}
}

// The complete page is accepted and applied, and a SET that carries none of the five is not a System-page
// SET and is not refused for it.
func TestTheFakeAcceptsACompleteSystemSetAndLeavesOtherPagesAlone(t *testing.T) {
	f := escptest.Start(t)
	c := dialFast(t, f, "secret")
	ctx := context.Background()
	if err := c.setTLVs(ctx, systemSetOp, systemSetOf()...); err != nil {
		t.Fatalf("the complete page must be accepted: %v", err)
	}
	info, err := c.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Hostname != "renamed" || info.DHCP || info.Gateway != netip.MustParseAddr("127.0.0.2") || info.Netmask != netip.MustParseAddr("255.255.0.0") {
		t.Fatalf("the complete page was not applied: %+v", info)
	}
	if err := c.SetLED(ctx, true); err != nil {
		t.Fatalf("a SET of another page must not be refused as a partial System-page SET: %v", err)
	}
	if n := f.WriteCount(); n != 2 {
		t.Fatalf("want 2 accepted writes, got %d", n)
	}
}

// PROTOCOL.md says the switch refuses a System-page SET that leaves out a setting of the page, so every
// writer of the page sends all five. The fake refuses a partial one, so a writer that drops a type fails
// here (and in every test of these writers) instead of passing against a fake that accepts anything.
func TestEveryWriterOfTheSystemPageSendsAllFiveSettings(t *testing.T) {
	want := []int{2, 4, 5, 6, 9}
	tests := []struct {
		name  string
		write func(t *testing.T, f *escptest.Switch) error
	}{
		{"SetHostname", func(t *testing.T, f *escptest.Switch) error {
			t.Helper()
			return dialFast(t, f, "secret").SetHostname(context.Background(), "new-name")
		}},
		{"SetIPConfig", func(t *testing.T, f *escptest.Switch) error {
			t.Helper()
			return dialFast(t, f, "secret").SetIPConfig(context.Background(), cfgOf("127.0.0.1", "255.255.0.0", "127.0.0.2"))
		}},
		{"EnableDHCP", func(t *testing.T, f *escptest.Switch) error {
			t.Helper()
			c, _ := dialLeasing(t, f)
			return c.EnableDHCP(context.Background())
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := escptest.Start(t)
			if err := tt.write(t, f); err != nil {
				t.Fatalf("the switch refused what the writer sent: %v", err)
			}
			sets := f.AllSets()
			if len(sets) != 1 {
				t.Fatalf("want one accepted write, got %d", len(sets))
			}
			var got []int
			for _, s := range sets[0] {
				got = append(got, int(s.Type))
			}
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Fatalf("the writer sent TLV types %v, want %v", got, want)
			}
		})
	}
}
