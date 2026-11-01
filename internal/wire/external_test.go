package wire_test

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/internal/wire"
)

func TestUnmarshalRejectsPacketsThatLieAboutTheirStructure(t *testing.T) {
	good := wire.Marshal(wire.Header{Op: wire.OpReply, Seq: 7}, []wire.TLV{{Type: 5, Value: []byte{1, 2}}})
	if _, tlvs, err := wire.Unmarshal(good); err != nil || len(tlvs) != 1 {
		t.Fatalf("control packet must parse: %v %v", tlvs, err)
	}
	t.Run("TLV longer than the packet", func(t *testing.T) {
		bad := append([]byte(nil), good...)
		bad[wire.HeaderLen+2], bad[wire.HeaderLen+3] = 0, 100 // claims 100 value bytes, 2 present
		if _, _, err := wire.Unmarshal(bad); err == nil || !strings.Contains(err.Error(), "truncated TLV type 5") {
			t.Fatalf("want a truncation error, got %v", err)
		}
	})
	t.Run("too short for a header", func(t *testing.T) {
		if _, _, err := wire.Unmarshal(good[:20]); err == nil || !strings.Contains(err.Error(), "not a valid packet") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("declared length does not match", func(t *testing.T) {
		bad := append([]byte(nil), good...)
		bad[21]++
		if _, _, err := wire.Unmarshal(bad); err == nil {
			t.Fatal("a wrong total length must be rejected")
		}
	})
	t.Run("unknown version", func(t *testing.T) {
		bad := append([]byte(nil), good...)
		bad[0] = 2
		if _, _, err := wire.Unmarshal(bad); err == nil {
			t.Fatal("version 2 must be rejected")
		}
	})
	t.Run("opcode out of range", func(t *testing.T) {
		bad := append([]byte(nil), good...)
		bad[1] = 5
		if _, _, err := wire.Unmarshal(bad); err == nil {
			t.Fatal("opcode 5 must be rejected")
		}
	})
}

func TestCString(t *testing.T) {
	for in, want := range map[string]string{"abc\x00def": "abc", "abc": "abc", "": "", "\x00": ""} {
		if got := wire.CString([]byte(in)); got != want {
			t.Errorf("cstring(%q) = %q, want %q", in, got, want)
		}
	}
}

// A ciphertext whose most significant words are zero is sent with a smaller
// word count, exactly like the vendor utility. The key was found by search and
// the expected TLV is cross-checked by decrypting it with the private key.
func TestRSASessionTLVTrimsLeadingZeroWords(t *testing.T) {
	key := []byte("AAAAahqn")
	want, _ := hex.DecodeString("0003569d648da2df0000")
	got := wire.RSASessionTLV(key)
	if !bytes.Equal(got, want) {
		t.Fatalf("got %x, want %x", got, want)
	}
	if got[1] != 3 {
		t.Fatalf("word count must be trimmed to 3, got %d", got[1])
	}
	if back := escptest.DecryptSessionKey(got); !bytes.Equal(back, key) {
		t.Fatalf("the trimmed TLV must still decrypt to the key, got %q", back)
	}
}

func TestOpNameNamesEveryOpAndNumbersTheRest(t *testing.T) {
	for op, want := range map[byte]string{wire.OpDiscovery: "discovery", wire.OpGet: "get", wire.OpReply: "reply", wire.OpSet: "set", wire.OpSetReply: "set-reply", 9: "op-9"} {
		if got := wire.OpName(op); got != want {
			t.Errorf("OpName(%d) = %q, want %q", op, got, want)
		}
	}
}

// The smallest packets that are packets: a header and the end marker; a TLV with no value; a TLV
// whose value ends exactly where the packet does. One byte less is refused.
func TestUnmarshalAcceptsThePacketsAtTheEdgeOfValidity(t *testing.T) {
	end := []byte{0xff, 0xff, 0, 0}
	build := func(op byte, payload []byte) []byte {
		b := wire.Marshal(wire.Header{Op: op, Seq: 1}, nil)[:wire.HeaderLen] // a header and nothing after it
		b = append(b, payload...)
		b[21] = byte(len(b)) // the declared length (all of these are shorter than 256)
		return b
	}
	empty := []byte{0, 5, 0, 0} // TLV 5, no value
	for _, tc := range []struct {
		name string
		pkt  []byte
		tlvs int
	}{
		{"only the end marker", build(wire.OpReply, end), 0},
		{"the highest opcode", build(wire.OpSetReply, end), 0},
		{"a TLV without a value", build(wire.OpReply, append(append([]byte(nil), empty...), end...)), 1},
		{"a TLV without a value and no end marker", build(wire.OpReply, empty), 1},
		{"a TLV that ends with the packet", build(wire.OpReply, []byte{0, 5, 0, 2, 9, 9}), 1},
	} {
		if _, tlvs, err := wire.Unmarshal(tc.pkt); err != nil || len(tlvs) != tc.tlvs {
			t.Errorf("%s: %d TLVs, %v; want %d", tc.name, len(tlvs), err, tc.tlvs)
		}
	}
	if wire.LooksLikeHeader(build(wire.OpReply, end)[:wire.HeaderLen+len(end)-1]) {
		t.Error("a packet one byte shorter than a header and an end marker is no packet")
	}
	if _, _, err := wire.Unmarshal(build(wire.OpSetReply+1, end)); err == nil {
		t.Error("the opcode after the highest was accepted")
	}
	if _, _, err := wire.Unmarshal(build(wire.OpReply, []byte{0, 5, 0, 3, 9, 9})); err == nil {
		t.Error("a TLV that claims one byte more than there is was accepted")
	}
}
