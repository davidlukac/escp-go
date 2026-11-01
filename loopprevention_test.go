package escp

import (
	"testing"

	"github.com/davidlukac/escp-go/internal/wire"
)

func TestParseLoopPrevention(t *testing.T) {
	on, err := parseLoopPrevention([]wire.TLV{tlvHex(t, wire.TLVLoopPrevention, "01")})
	if err != nil || !on {
		t.Fatalf("got %v, %v", on, err)
	}
	off, err := parseLoopPrevention([]wire.TLV{tlvHex(t, wire.TLVLoopPrevention, "00")})
	if err != nil || off {
		t.Fatalf("got %v, %v", off, err)
	}
	if _, err := parseLoopPrevention(nil); err == nil {
		t.Fatal("expected error for empty reply")
	}
	// a value of the wrong length is not the flag: ignored, never read past its end
	for _, bad := range []string{"", "0101"} {
		if on, err := parseLoopPrevention([]wire.TLV{tlvHex(t, wire.TLVLoopPrevention, bad)}); err == nil {
			t.Errorf("a flag of %q bytes was accepted: %v", bad, on)
		}
	}
}
