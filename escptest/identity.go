package escptest

import (
	"github.com/davidlukac/escp-go/internal/wire"
)

// handleDiscovery answers the unauthenticated probe with the switch's identity and address: the
// scripted reply when a test replaced it, else what the model holds (a lease while DHCP is on, an old
// address while stale replies are due).
func (f *Switch) handleDiscovery(r request) {
	f.mu.Lock()
	override := f.faults.DiscoverReply
	f.mu.Unlock()
	if override != nil {
		f.reply(r, wire.OpReply, 0, toTLVs(override), false)
		return
	}
	f.mu.Lock()
	addr, firmware := f.addr, f.firmware
	mac := f.macAddr()
	dhcp := byte(0) // a static address, like a switch that was set up by hand
	if f.dhcp {
		dhcp = 1
		addr = map[wire.TLVType][]byte{wire.TLVIP: f.lease.IP[:], wire.TLVNetmask: f.lease.Mask[:], wire.TLVGateway: f.lease.Gateway[:]}
	}
	name := "TL-SG108E-fake"
	if f.hostname != "" {
		name = f.hostname
	}
	if f.staleDiscoveries > 0 {
		f.staleDiscoveries--
		addr = map[wire.TLVType][]byte{wire.TLVIP: {127, 0, 0, 9}, wire.TLVNetmask: {255, 255, 255, 0}, wire.TLVGateway: {127, 0, 0, 254}}
	}
	f.mu.Unlock()
	ip, mask, gw := []byte{127, 0, 0, 1}, []byte{255, 255, 255, 0}, []byte{127, 0, 0, 254}
	if addr != nil {
		ip, mask, gw = addr[wire.TLVIP], addr[wire.TLVNetmask], addr[wire.TLVGateway]
	}
	f.reply(r, wire.OpReply, 0, []wire.TLV{
		{Type: wire.TLVModel, Value: []byte("TL-SG108E\x00")}, {Type: wire.TLVHostname, Value: []byte(name + "\x00")},
		{Type: wire.TLVMAC, Value: mac[:]}, {Type: wire.TLVIP, Value: ip},
		{Type: wire.TLVNetmask, Value: mask}, {Type: wire.TLVGateway, Value: gw},
		{Type: wire.TLVFirmware, Value: []byte(firmware + "\x00")}, {Type: wire.TLVHardware, Value: []byte("TL-SG108E 6.0\x00")},
		{Type: wire.TLVDHCP, Value: []byte{dhcp}}, {Type: wire.TLVFactoryDefault, Value: []byte{1}},
	}, false)
}

// Lease is what the modeled switch gets from its DHCP server when its DHCP flag is switched on.
// The fake can only answer from 127.0.0.1, so a lease that differs from the static address in
// the mask or the gateway is what tells the two apart, as in SetIPConfig's tests.
type Lease struct {
	IP, Mask, Gateway [4]byte
	// Silence is how many packets the switch swallows after DHCP is switched on, while it asks for
	// the lease (a real switch was silent for about two seconds).
	Silence int
}

// DefaultLease is the lease EnableModel gives the switch: the fake's own address with another mask
// and gateway.
func DefaultLease() Lease {
	return Lease{IP: [4]byte{127, 0, 0, 1}, Mask: [4]byte{255, 255, 0, 0}, Gateway: [4]byte{127, 0, 0, 2}}
}

// SetLease changes the lease the fake gives. The fake models the DHCP flag the way a real switch
// behaves (measured on firmware 20250710) from the start, with DefaultLease: it starts static; a
// system SET that carries the flag on makes it lease l (discovery reports the lease and DHCP on; a
// static address in later SETs is ignored); a SET with the flag off gives it the static address of
// that SET.
func (f *Switch) SetLease(l Lease) { f.mu.Lock(); f.lease = &l; f.mu.Unlock() }

// DefaultMAC is the address the fake reports unless SetMAC says otherwise (the documentation range, RFC 7042).
func DefaultMAC() [6]byte { return [6]byte{0x00, 0x00, 0x5e, 0x00, 0x53, 0x02} }

// macAddr is the MAC the switch reports; callers hold f.mu.
func (f *Switch) macAddr() [6]byte {
	if f.mac == ([6]byte{}) {
		return DefaultMAC()
	}
	return f.mac
}

// SetMAC changes the MAC address the switch reports in its headers and in discovery replies,
// to make several fakes distinguishable or to play a different device at the same address.
func (f *Switch) SetMAC(mac [6]byte) { f.mu.Lock(); f.mac = mac; f.mu.Unlock() }
