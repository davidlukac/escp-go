package escp

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"

	"github.com/davidlukac/escp-go/internal/wire"
)

// Fuzz targets for what fuzz_test.go does not reach: the amount of work a reply can cause, the
// discovery reply, the address triple, and the reassembly of the configuration file. FuzzSettingParsers
// only asks that a parser does not panic; these ask what it returned (DC-L06: a reply cannot make the
// client allocate or loop beyond what its size allows). `go test` runs the seeds; run
// `go test -fuzz=<Name> -fuzztime=30s .` to explore further.

// settingTLVTypes are the TLV types the settings parsers read.
var settingTLVTypes = []wire.TLVType{
	wire.TLVPorts, wire.TLVPortStats, wire.TLVLAG, wire.TLVIGMPEnabled, wire.TLVIGMPSuppression, wire.TLVIGMPGroups, wire.TLVMirror,
	wire.TLVVLANMTU, wire.TLVVLANPortBased, wire.TLVVLANPortBasedVID, wire.TLVVLANPortBasedMax, wire.TLVVLAN8021Q, wire.TLVVLAN8021QVLAN,
	wire.TLVVLAN8021QPVID, wire.TLVVLAN8021QMax, wire.TLVQoSMode, wire.TLVQoSPriority, wire.TLVBandwidthIngress, wire.TLVBandwidthEgress,
	wire.TLVStormControl, wire.TLVLoopPrevention, wire.TLVPortCount, wire.TLVAutoSave,
}

// infoTLVTypes are the TLV types a discovery reply carries.
var infoTLVTypes = []wire.TLVType{
	wire.TLVModel, wire.TLVHostname, wire.TLVMAC, wire.TLVIP, wire.TLVNetmask, wire.TLVGateway, wire.TLVFirmware, wire.TLVHardware,
	wire.TLVDHCP, wire.TLVFactoryDefault,
}

// resultCounts runs every settings parser over tlvs and returns how many entries each produced; it
// reports a parser that gave entries together with an error where the parser's contract is "a list
// or an error".
func resultCounts(t *testing.T, tlvs []wire.TLV) map[string]int {
	t.Helper()
	counts := map[string]int{}
	ports, err := parsePorts(tlvs)
	counts["ports"] = len(ports)
	requireNoneOnError(t, "ports", len(ports), err)
	stats, err := parsePortStats(tlvs)
	counts["port statistics"] = len(stats)
	requireNoneOnError(t, "port statistics", len(stats), err)
	lags, err := parseLAGs(tlvs)
	counts["LAGs"] = len(lags)
	requireNoneOnError(t, "LAGs", len(lags), err)
	storm, err := parseStorm(tlvs)
	counts["storm control"] = len(storm)
	requireNoneOnError(t, "storm control", len(storm), err)
	bw, err := parseBandwidth(tlvs)
	counts["bandwidth"] = len(bw)
	requireNoneOnError(t, "bandwidth", len(bw), err)
	igmp, _ := parseIGMP(tlvs)
	counts["IGMP groups"] = len(igmp.Groups)
	qos, _ := parseQoS(tlvs)
	counts["QoS priorities"] = len(qos.Priorities)
	dot1q, _ := parseDot1Q(tlvs)
	counts["802.1Q VLANs"] = len(dot1q.VLANs)
	counts["802.1Q PVIDs"] = len(dot1q.PVIDs)
	pvlan, _ := parsePortBasedVLAN(tlvs)
	counts["port-based VLANs"] = len(pvlan.VLANs)
	_, _ = parseMirror(tlvs)
	_, _ = parseMTUVLAN(tlvs)
	_, _ = parseLoopPrevention(tlvs)
	_, _ = parseSystem(tlvs, tlvs)
	return counts
}

func requireNoneOnError(t *testing.T, name string, n int, err error) {
	t.Helper()
	if err != nil && n != 0 {
		t.Fatalf("%s: %d entries returned beside the error %v", name, n, err)
	}
}

// FuzzParserBounds: n copies of one TLV, any type and any value, make no parser return more entries
// than there are TLVs (an IGMP table: than there are group entries in all the values together), so
// the work and the memory of a read are bounded by the size of the reply.
func FuzzParserBounds(f *testing.F) {
	f.Add(int16(wire.TLVPorts), mustHex(f, "01010101060000"), uint8(8))
	f.Add(int16(wire.TLVPortStats), mustHex(f, "0101061434f1b7000000002873fb8a00000000"), uint8(8))
	f.Add(int16(wire.TLVLAG), mustHex(f, "0100000003"), uint8(4))
	f.Add(int16(wire.TLVIGMPGroups), mustHex(f, "effffffa000100000009"), uint8(2))
	f.Add(int16(wire.TLVIGMPGroups), bytes.Repeat(mustHex(f, "effffffa000100000009"), 100), uint8(8))
	f.Add(int16(wire.TLVVLAN8021QVLAN), mustHex(f, "000a0000008300000080696f7400"), uint8(5))
	f.Add(int16(wire.TLVVLAN8021QPVID), mustHex(f, "010001"), uint8(8))
	f.Add(int16(wire.TLVQoSPriority), mustHex(f, "0100"), uint8(8))
	f.Add(int16(wire.TLVBandwidthIngress), mustHex(f, "010000000000"), uint8(8))
	f.Add(int16(wire.TLVStormControl), mustHex(f, "010000000000000000"), uint8(8))
	f.Add(int16(wire.TLVVLANPortBasedVID), mustHex(f, "01000000ff"), uint8(8))
	f.Add(int16(0), []byte{}, uint8(0))
	f.Fuzz(func(t *testing.T, rawType int16, value []byte, n uint8) {
		count := int(n % 9)
		for _, ty := range append([]wire.TLVType{wire.TLVType(rawType)}, settingTLVTypes...) {
			tlvs := make([]wire.TLV, count)
			for i := range tlvs {
				tlvs[i] = wire.TLV{Type: ty, Value: value}
			}
			for name, got := range resultCounts(t, tlvs) {
				limit := count
				if name == "IGMP groups" {
					limit = count * (len(value) / igmpGroupLen)
				}
				if got > limit {
					t.Fatalf("%d TLVs of type %d (%d bytes each) gave %d %s, at most %d", count, ty, len(value), got, name, limit)
				}
			}
		}
	})
}

// packetOf frames a TLV payload as a reply packet whose length field is right, so that the fuzzer
// explores the TLV list and not the length check that FuzzUnmarshal already covers.
func packetOf(payload []byte) []byte {
	const lengthOffset = 20 // the 16-bit length field of the header (PROTOCOL.md)
	b := wire.Marshal(wire.Header{Op: wire.OpReply}, nil)[:wire.HeaderLen]
	b = append(b, payload...)
	binary.BigEndian.PutUint16(b[lengthOffset:], uint16(len(b)))
	return b
}

// tlvPayload is the TLV list of golden values as the bytes that follow the header.
func tlvPayload(tlvs ...wire.TLV) []byte {
	var p []byte
	for _, tl := range tlvs {
		p = binary.BigEndian.AppendUint16(p, uint16(tl.Type))
		p = binary.BigEndian.AppendUint16(p, uint16(len(tl.Value)))
		p = append(p, tl.Value...)
	}
	return p
}

// FuzzReplyPayload: the TLV list of a reply, whatever it holds, is decoded by every parser at once,
// like a read that asks for several types. The entries all together are no more than the TLVs, the
// IGMP entries no more than the bytes, and the discovery fields are what the TLVs carry.
func FuzzReplyPayload(f *testing.F) {
	f.Add(tlvPayload(th(wire.TLVPorts, "01010101060000"), th(wire.TLVPorts, "02010101060000"), th(wire.TLVLAG, "0100000003"),
		th(wire.TLVMirror, "01080000000300000004"), th(wire.TLVQoSMode, "01"), th(wire.TLVVLAN8021Q, "01")))
	f.Add(tlvPayload(th(wire.TLVMAC, "00005e005301"), th(wire.TLVIP, "c0000201"), th(wire.TLVNetmask, "ffffff00"), th(wire.TLVGateway, "c0000201"),
		wire.TLV{Type: wire.TLVModel, Value: []byte("TL-SG108E\x00")}, th(wire.TLVDHCP, "00")))
	f.Add(tlvPayload(th(wire.TLVIGMPGroups, "effffffa000100000009effffffb000100000009")))
	f.Add([]byte{0xff, 0xff, 0, 0})
	f.Add([]byte{0, 1, 0xff, 0xff, 0, 0})
	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) < len(wire.ConfigMagic) { // a packet holds at least the end marker's four bytes
			payload = append(payload, make([]byte, len(wire.ConfigMagic)-len(payload))...)
		}
		_, tlvs, err := wire.Unmarshal(packetOf(payload))
		if err != nil {
			return
		}
		total := 0
		for name, got := range resultCounts(t, tlvs) {
			if name != "IGMP groups" && got > len(tlvs) {
				t.Fatalf("%d TLVs gave %d %s", len(tlvs), got, name)
			}
			total += got
		}
		if total > len(payload) {
			t.Fatalf("a payload of %d bytes gave %d entries", len(payload), total)
		}
		checkInfo(t, tlvs)
	})
}

// checkInfo holds what infoFromTLVs promises for the TLVs it was given and for the triple that
// ipConfigFromInfo takes from the result.
func checkInfo(t *testing.T, tlvs []wire.TLV) {
	t.Helper()
	info := infoFromTLVs(tlvs)
	for _, s := range []string{info.Model, info.Hardware, string(info.Hostname), info.Firmware.String()} {
		if bytes.ContainsRune([]byte(s), 0) {
			t.Fatalf("a text field of the reply holds a NUL: %q", s)
		}
	}
	for _, a := range []struct {
		name string
		got  interface{ IsValid() bool }
		ty   wire.TLVType
		ok   bool
	}{{"IP", info.IP, wire.TLVIP, info.IP.Is4()}, {"netmask", info.Netmask, wire.TLVNetmask, info.Netmask.Is4()}, {"gateway", info.Gateway, wire.TLVGateway, info.Gateway.Is4()}} {
		if a.got.IsValid() && !a.ok {
			t.Fatalf("the %s of a reply is valid but not IPv4", a.name)
		}
		if a.got.IsValid() && !slices.ContainsFunc(tlvs, func(x wire.TLV) bool { return x.Type == a.ty && len(x.Value) == 4 }) {
			t.Fatalf("the %s was set without a four-byte TLV", a.name)
		}
	}
	if !info.MAC.IsZero() && !slices.ContainsFunc(tlvs, func(x wire.TLV) bool { return x.Type == wire.TLVMAC && bytes.Equal(x.Value, info.MAC[:]) }) {
		t.Fatalf("the MAC %v is in no TLV", info.MAC)
	}
	if cfg, err := ipConfigFromInfo(info); err != nil {
		if cfg.IP.IsValid() || cfg.Netmask.IsValid() || cfg.Gateway.IsValid() {
			t.Fatalf("a refused triple still carries addresses: %v", cfg)
		}
	} else if !cfg.IP.Is4() || !cfg.Netmask.Is4() || !cfg.Gateway.Is4() {
		t.Fatalf("ipConfigFromInfo accepted %v, which is not three IPv4 addresses", cfg)
	}
}

// FuzzInfoFromTLVs: any value under each type of a discovery reply, and under a fuzzed type, gives an
// Info whose fields are what that one TLV allows: text never longer than the value and without a NUL,
// an address only from four bytes, a MAC only from six, and nothing else set.
func FuzzInfoFromTLVs(f *testing.F) {
	f.Add(int16(wire.TLVMAC), mustHex(f, "00005e005301"))
	f.Add(int16(wire.TLVIP), mustHex(f, "c0000201"))
	f.Add(int16(wire.TLVFirmware), []byte("1.0.0 Build 20230218 Rel.50633\x00"))
	f.Add(int16(wire.TLVHostname), []byte("sw-01\x00\x00"))
	f.Add(int16(wire.TLVDHCP), []byte{1})
	f.Add(int16(wire.TLVMAC), []byte{1, 2, 3})
	f.Add(int16(wire.TLVIP), bytes.Repeat([]byte{0xff}, 16))
	f.Add(int16(-1), []byte{})
	f.Fuzz(func(t *testing.T, rawType int16, value []byte) {
		for _, ty := range append([]wire.TLVType{wire.TLVType(rawType)}, infoTLVTypes...) {
			tlvs := []wire.TLV{{Type: ty, Value: value}}
			checkInfo(t, tlvs)
			info := infoFromTLVs(tlvs)
			for _, s := range []string{info.Model, info.Hardware, string(info.Hostname), info.Firmware.String()} {
				if len(s) > len(value) {
					t.Fatalf("a text of %d bytes from a value of %d", len(s), len(value))
				}
			}
			if len(value) != 4 && (info.IP.IsValid() || info.Netmask.IsValid() || info.Gateway.IsValid()) {
				t.Fatalf("type %d: %d bytes gave the addresses %v %v %v", ty, len(value), info.IP, info.Netmask, info.Gateway)
			}
			if len(value) != 6 && !info.MAC.IsZero() {
				t.Fatalf("type %d: %d bytes gave the MAC %v", ty, len(value), info.MAC)
			}
			if (info.DHCP || info.FactoryDefault) && (len(value) != 1 || value[0] == 0) {
				t.Fatalf("type %d: a flag set from % x", ty, value)
			}
		}
	})
}

// splitInto cuts b into packets of n bytes, the last one with what remains.
func splitInto(b []byte, n int) [][]byte {
	var parts [][]byte
	for len(b) > n {
		parts, b = append(parts, b[:n]), b[n:]
	}
	return append(parts, b)
}

// FuzzAssembleConfig: the packets of a configuration download are any bytes in any sizes. What is
// accepted is the packets joined, starts with the magic, has no short packet before the last and no
// longer last one; a file cut into equal packets (the way the switch sends it) always comes back
// whole.
func FuzzAssembleConfig(f *testing.F) {
	// The switch's file is about 2300 bytes in packets of 1400 (measured); the target keeps the size small, so the seed has its shape.
	file := append(bytes.Clone(wire.ConfigMagic), bytes.Repeat([]byte{0x5a}, maxFuzzFile-len(wire.ConfigMagic))...)
	f.Add(file, uint16(399), uint16(0))
	f.Add(file, uint16(1), uint16(0))
	f.Add(file, uint16(maxFuzzFile), uint16(7))
	f.Add(wire.ConfigMagic, uint16(4), uint16(0))
	f.Add([]byte{}, uint16(1), uint16(0))
	f.Add(file[:len(file)-1], uint16(1400), uint16(5))
	f.Fuzz(func(t *testing.T, data []byte, size, cut uint16) {
		if len(data) > maxFuzzFile {
			data = data[:maxFuzzFile] // a file of any size is the same code; one packet per byte is slow
		}
		n := int(size)%maxFuzzPacket + 1
		parts := splitInto(data, n)
		got, err := assembleConfig(parts)
		whole := len(data) > len(wire.ConfigMagic) && bytes.HasPrefix(data, wire.ConfigMagic)
		if whole != (err == nil) {
			t.Fatalf("a %d-byte file with the magic %v in packets of %d: error %v", len(data), bytes.HasPrefix(data, wire.ConfigMagic), n, err)
		}
		if err == nil && !bytes.Equal(got, data) {
			t.Fatalf("the file came back changed: %d bytes -> %d", len(data), len(got))
		}
		// A packet cut short in front of the last one, or a longer last one, is never a file.
		if len(parts) > 1 && cut > 0 {
			short := slices.Clone(parts)
			short[0] = short[0][:int(cut)%len(short[0])]
			if _, err := assembleConfig(short); err == nil && len(short) > 2 {
				t.Fatalf("a short first packet (%d, then %d) was accepted", len(short[0]), len(short[1]))
			}
		}
		// Whatever it is given, the result is no longer than the packets together.
		if err == nil && len(got) > len(data) {
			t.Fatalf("%d bytes from %d", len(got), len(data))
		}
	})
}

// What a fuzz input of FuzzAssembleConfig may pick: the packet size (the switch sends 1400 bytes) and
// the size of the file (the switch's is about 2300).
const (
	maxFuzzPacket = 1500
	maxFuzzFile   = 1024
)
