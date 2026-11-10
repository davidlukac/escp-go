package wire

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

// More fuzz targets for what arrives over the network: the inverse of Unmarshal, the header check
// and the NUL-terminated strings of a reply. FuzzUnmarshal (packet_fuzz_test.go) holds the first.
// `go test` runs the seeds; `go test -fuzz=<Name> -fuzztime=30s ./internal/wire` explores further.

// maxFuzzValue keeps a generated TLV value inside the 16-bit length field of the wire format.
const maxFuzzValue = 2000

// sameTLVs reports whether two lists hold the same types and values.
func sameTLVs(a, b []TLV) bool {
	return slices.EqualFunc(a, b, func(x, y TLV) bool { return x.Type == y.Type && bytes.Equal(x.Value, y.Value) })
}

// FuzzUnmarshalCount: what Unmarshal accepts is bounded by the datagram (every TLV costs at least its
// four header bytes, so a packet cannot make the list longer than its size allows), agrees with
// LooksLikeHeader, and survives being written again: Marshal then Unmarshal gives the same header
// fields and the same TLVs. Anything shorter than a packet is refused, never indexed.
func FuzzUnmarshalCount(f *testing.F) {
	f.Add(seedPacket())
	f.Add(Marshal(Header{Op: OpSet, Seq: 7, ErrCode: -1, Fragment: 2}, []TLV{{TLVUsername, []byte("admin")}, {TLVPassword, nil}, {TLVLED, []byte{0}}}))
	f.Add(Marshal(Header{}, nil))
	f.Add(Marshal(Header{Op: OpReply}, []TLV{{TLVType(-1), nil}, {TLVLED, []byte{1}}}))
	f.Add([]byte{})
	f.Add(make([]byte, HeaderLen+len(packetEnd)-1))
	f.Add(bytes.Repeat([]byte{0x01}, 300))
	f.Fuzz(func(t *testing.T, data []byte) {
		h, tlvs, err := Unmarshal(data)
		if (err == nil) && !LooksLikeHeader(data) {
			t.Fatalf("Unmarshal accepted % x, which LooksLikeHeader refuses", data)
		}
		if len(data) < HeaderLen+len(packetEnd) && err == nil {
			t.Fatalf("Unmarshal accepted %d bytes, fewer than a header and an end marker", len(data))
		}
		if err != nil {
			return
		}
		if limit := (len(data) - HeaderLen) / TLVHeaderLen; len(tlvs) > limit {
			t.Fatalf("%d TLVs from a payload that holds at most %d", len(tlvs), limit)
		}
		// The length field is derived (Marshal fills it in from the list it is given), so it is not compared.
		h2, tlvs2, err := Unmarshal(Marshal(h, tlvs))
		if h2.Length, h.Length = 0, 0; err != nil || h2 != h || !sameTLVs(tlvs, tlvs2) {
			t.Fatalf("a packet read and written again changed: %+v %v -> %+v %v (%v)", h, tlvs, h2, tlvs2, err)
		}
	})
}

// FuzzMarshalUnmarshal: the packet of any header and TLV list that Marshal builds reads back as the
// same header (the length field aside, which Marshal fills in) and the same TLVs, unless the list holds
// the end marker itself, which ends a list by definition.
func FuzzMarshalUnmarshal(f *testing.F) {
	f.Add(byte(OpReply), uint16(59), int32(0), uint16(0), int16(TLVLED), []byte{1}, uint8(1))
	f.Add(byte(OpSetReply), uint16(0xffff), int32(ErrCodeKeyAccepted), uint16(1), int16(TLVConfigFile), bytes.Repeat([]byte{7}, 300), uint8(3))
	f.Add(byte(OpDiscovery), uint16(0), int32(-1), uint16(0), int16(0), []byte{}, uint8(0))
	f.Add(byte(OpGet), uint16(1), int32(0), uint16(0), int16(-1), []byte{}, uint8(2))
	f.Fuzz(func(t *testing.T, op byte, seq uint16, errCode int32, fragment uint16, typ int16, value []byte, n uint8) {
		if len(value) > maxFuzzValue {
			value = value[:maxFuzzValue]
		}
		var tlvs []TLV
		for range n % 8 {
			tlvs = append(tlvs, TLV{Type: TLVType(typ), Value: value})
		}
		h := Header{Op: op, Seq: seq, ErrCode: errCode, Fragment: fragment, SwitchMAC: [MACLen]byte{0, 0, 0x5e, 0, 0x53, 1}}
		got, back, err := Unmarshal(Marshal(h, tlvs))
		if op > OpSetReply {
			if err == nil {
				t.Fatalf("a packet with operation %d was accepted", op)
			}
			return
		}
		if err != nil {
			t.Fatalf("a packet Marshal built was refused: %v", err)
		}
		if got.Length = 0; got != h {
			t.Fatalf("the header changed: %+v -> %+v", h, got)
		}
		if typ == tlvEndType && len(value) == 0 && len(tlvs) > 0 {
			if len(back) != 0 {
				t.Fatalf("an end marker in the list did not end it: %v", back)
			}
			return
		}
		if !sameTLVs(tlvs, back) {
			t.Fatalf("the TLVs changed: %v -> %v", tlvs, back)
		}
	})
}

// FuzzCString: the text of a reply field is the bytes up to the first NUL, never more than was
// given and never containing a NUL; text without a NUL comes back whole.
func FuzzCString(f *testing.F) {
	for _, s := range []string{"TL-SG108E\x00\x00\x00", "no terminator", "", "\x00", "a\x00b\x00c", "\xff\xfe\x00"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		s := CString(b)
		if len(s) > len(b) || strings.ContainsRune(s, 0) || !bytes.HasPrefix(b, []byte(s)) {
			t.Fatalf("CString(%q) = %q", b, s)
		}
		if !bytes.Contains(b, []byte{0}) && s != string(b) {
			t.Fatalf("CString(%q) = %q cut text that has no NUL", b, s)
		}
		if len(s) < len(b) && b[len(s)] != 0 {
			t.Fatalf("CString(%q) = %q stopped before a NUL", b, s)
		}
	})
}
