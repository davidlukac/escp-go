package wire

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/davidlukac/escp-go/internal/invalid"
)

// roomForOneValue is the largest value a packet of one TLV can hold: the packet limit less the
// header, the TLV's own header and the end marker.
const roomForOneValue = MaxPacket - HeaderLen - TLVHeaderLen - TLVHeaderLen

func valueOf(n int) []byte { return bytes.Repeat([]byte{0xA5}, n) }

// guarantee: CheckLengths
func TestCheckLengthsBoundsTheValueAndThePacket(t *testing.T) {
	for _, tc := range []struct {
		name string
		tlvs []TLV
		want string // a part of the error; empty means accepted
	}{
		{"no TLVs", nil, ""},
		{"an empty value", []TLV{{TLVLED, nil}}, ""},
		{"the largest value a packet holds", []TLV{{TLVConfigFile, valueOf(roomForOneValue)}}, ""},
		{"one byte more than the packet holds", []TLV{{TLVConfigFile, valueOf(roomForOneValue + 1)}}, "packet"},
		{"a value at the TLV limit overflows the packet, not the field", []TLV{{TLVConfigFile, valueOf(MaxTLVValue)}}, "packet"},
		{"a value one over the TLV limit", []TLV{{TLVConfigFile, valueOf(MaxTLVValue + 1)}}, "TLV type 768"},
		{"a value of 70000 bytes", []TLV{{TLVConfigFile, valueOf(70000)}}, "TLV type 768"},
		{"several values that fit together", []TLV{{TLVUsername, valueOf(100)}, {TLVPassword, valueOf(roomForOneValue - 100 - TLVHeaderLen)}}, ""},
		{"several values that overflow together", []TLV{{TLVUsername, valueOf(100)}, {TLVPassword, valueOf(roomForOneValue - 100 - TLVHeaderLen + 1)}}, "packet"},
		{"the first value too large, the second small", []TLV{{TLVPassword, valueOf(MaxTLVValue + 1)}, {TLVLED, []byte{1}}}, "TLV type 514"},
		{"the second value too large", []TLV{{TLVLED, []byte{1}}, {TLVPassword, valueOf(MaxTLVValue + 1)}}, "TLV type 514"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckLengths(tc.tlvs)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("refused a request that fits: %v", err)
				}
				return
			}
			if !errors.Is(err, invalid.Err) {
				t.Fatalf("got %v, want an error that matches the invalid-argument class", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not name %q", err, tc.want)
			}
			if !strings.Contains(err.Error(), fmt.Sprint(MaxPacket)) { // MaxTLVValue and MaxPacket are the same number: the limit is always named
				t.Fatalf("error %q does not name the limit %d", err, MaxPacket)
			}
			if strings.Contains(err.Error(), string(valueOf(8))) {
				t.Fatalf("error %q echoes the value", err)
			}
		})
	}
}

// What CheckLengths accepts at its limit is a packet whose length field says what it holds, and
// the parser reads the TLVs back: the sum of the bounds is exactly the 16-bit field.
func TestMarshalOfTheLargestAcceptedPacketRoundTrips(t *testing.T) {
	tlvs := []TLV{{TLVUsername, valueOf(10)}, {TLVConfigFile, valueOf(roomForOneValue - 10 - TLVHeaderLen)}}
	if err := CheckLengths(tlvs); err != nil {
		t.Fatal(err)
	}
	pkt := Marshal(Header{Op: OpSet, Seq: 7}, tlvs)
	if len(pkt) != MaxPacket {
		t.Fatalf("the largest accepted packet is %d bytes, want %d", len(pkt), MaxPacket)
	}
	h, got, err := Unmarshal(pkt)
	if err != nil {
		t.Fatal(err)
	}
	if int(h.Length) != len(pkt) {
		t.Fatalf("length field %d, packet %d bytes", h.Length, len(pkt))
	}
	if len(got) != len(tlvs) || !bytes.Equal(got[0].Value, tlvs[0].Value) || !bytes.Equal(got[1].Value, tlvs[1].Value) {
		t.Fatal("the TLVs read back differ from the ones marshaled")
	}
}
