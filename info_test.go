package escp

import (
	"context"
	"net/netip"
	"testing"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
)

func TestInfoFromReplyToleratesMissingAndMalformedFields(t *testing.T) {
	if got := infoFromReply(reply{}); got != (Info{}) {
		t.Fatalf("an empty reply must give an empty Info, got %+v", got)
	}
	got := infoFromReply(reply{tlvs: []wire.TLV{
		{Type: wire.TLVIP, Value: []byte{192, 168, 1}},           // 3 bytes: not an address
		{Type: wire.TLVNetmask, Value: []byte{255, 255, 255, 0}}, // fine
		{Type: wire.TLVMAC, Value: []byte{1, 2, 3}},              // 3 bytes: not a MAC
		{Type: wire.TLVDHCP, Value: []byte{1, 1}},                // 2 bytes: ignored
		{Type: wire.TLVFactoryDefault, Value: nil},               // empty: ignored
		{Type: wire.TLVModel, Value: []byte("TL-SG108E")},        // no NUL terminator
	}})
	want := Info{Model: "TL-SG108E", Netmask: netip.MustParseAddr("255.255.255.0")}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// An address TLV holds four bytes. A reply that carries sixteen (an IPv6 address) or eight is no
// IPv4 address, and must not become one.
func TestInfoFromReplyReadsOnlyFourByteAddresses(t *testing.T) {
	got := infoFromReply(reply{tlvs: []wire.TLV{
		{Type: wire.TLVIP, Value: netip.MustParseAddr("2001:db8::1").AsSlice()},
		{Type: wire.TLVNetmask, Value: make([]byte, 8)},
		{Type: wire.TLVGateway, Value: []byte{192, 0, 2, 1}},
	}})
	if got.IP.IsValid() || got.Netmask.IsValid() || got.Gateway != netip.MustParseAddr("192.0.2.1") {
		t.Fatalf("got ip %v, mask %v, gateway %v", got.IP, got.Netmask, got.Gateway)
	}
}

func TestInfo(t *testing.T) {
	c := dialFake(t, escptest.Start(t), "secret")
	info, err := c.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Model != "TL-SG108E" || info.Hostname != "TL-SG108E-fake" || info.MAC.String() != "00:00:5e:00:53:02" ||
		info.Hardware != "TL-SG108E 6.0" || info.IP.String() != "127.0.0.1" || info.DHCP || !info.FactoryDefault {
		t.Fatalf("unexpected info: %+v", info)
	}
}
