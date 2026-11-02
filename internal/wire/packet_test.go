package wire

import (
	"encoding/binary"
	"fmt"
	"testing"
)

// A packet is at least a header and the end marker. The test builds buffers whose length field is
// right for their own length, so only the minimum length can refuse the short one (a buffer cut from
// a longer packet is refused by the length field alone).
func TestLooksLikeHeaderNeedsRoomForTheHeaderAndTheEndMarker(t *testing.T) {
	for _, tc := range []struct {
		size int
		want bool
	}{
		{HeaderLen + len(packetEnd), true},
		{HeaderLen + len(packetEnd) - 1, false},
		{HeaderLen, false},
		{HeaderLen - len(packetEnd), false},
	} {
		t.Run(fmt.Sprintf("size %d", tc.size), func(t *testing.T) {
			b := make([]byte, tc.size)
			b[0], b[offOp] = protocolVersion, OpGet
			binary.BigEndian.PutUint16(b[offLength:], uint16(tc.size))
			if got := LooksLikeHeader(b); got != tc.want {
				t.Errorf("a well-formed buffer of %d bytes: LooksLikeHeader = %v, want %v", tc.size, got, tc.want)
			}
		})
	}
}
