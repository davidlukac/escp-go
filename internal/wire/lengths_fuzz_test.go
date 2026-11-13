package wire

import (
	"bytes"
	"errors"
	"testing"

	"github.com/davidlukac/escp-go/internal/invalid"
)

// FuzzMarshalAfterCheckLengths: whatever CheckLengths accepts, Marshal encodes into a packet that
// Unmarshal reads back unchanged and whose length field is right; whatever it refuses is refused with
// the invalid-argument class. Sizes are generated and not bytes, so the sizes around the 16-bit limits
// are reached without allocating much: `go test` runs the seeds, `go test -fuzz=FuzzMarshalAfterCheckLengths ./internal/wire` explores.
func FuzzMarshalAfterCheckLengths(f *testing.F) {
	f.Add(uint16(0), uint32(0), uint32(0))
	f.Add(uint16(12), uint32(1), uint32(2))
	f.Add(uint16(768), uint32(roomForOneValue), uint32(0))
	f.Add(uint16(768), uint32(roomForOneValue+1), uint32(0))
	f.Add(uint16(514), uint32(MaxTLVValue), uint32(0))
	f.Add(uint16(514), uint32(MaxTLVValue+1), uint32(0))
	f.Add(uint16(2), uint32(30000), uint32(30000))
	f.Fuzz(func(t *testing.T, typ uint16, first, second uint32) {
		// two values, each up to about 128 KiB, so that both bounds are crossed from either side
		tlvs := []TLV{
			{Type: TLVType(typ), Value: bytes.Repeat([]byte{0x5A}, int(first%(2*MaxPacket)))},
			{Type: TLVType(typ) + 1, Value: bytes.Repeat([]byte{0xA5}, int(second%(2*MaxPacket)))},
		}
		if err := CheckLengths(tlvs); err != nil {
			if !errors.Is(err, invalid.Err) {
				t.Fatalf("refused with %v, which is not the invalid-argument class", err)
			}
			return
		}
		pkt := Marshal(Header{Op: OpSet, Seq: 1}, tlvs)
		h, got, err := Unmarshal(pkt)
		if err != nil {
			t.Fatalf("a packet CheckLengths accepted does not parse: %v", err)
		}
		if int(h.Length) != len(pkt) || len(pkt) > MaxPacket {
			t.Fatalf("length field %d, packet %d bytes, limit %d", h.Length, len(pkt), MaxPacket)
		}
		if len(got) != len(tlvs) {
			t.Fatalf("%d TLVs read back, %d marshaled", len(got), len(tlvs))
		}
		for i := range tlvs {
			if got[i].Type != tlvs[i].Type || !bytes.Equal(got[i].Value, tlvs[i].Value) {
				t.Fatalf("TLV %d differs after the round trip", i)
			}
		}
	})
}
