package wire

import (
	"math"

	"github.com/davidlukac/escp-go/internal/invalid"
)

// The wire carries two lengths in 16 bits, so a packet cannot say more than they can hold.
const (
	// MaxTLVValue is the longest value one TLV can carry: the length field of a TLV header is 16 bits
	// (PROTOCOL.md, the TLV layout).
	MaxTLVValue = math.MaxUint16
	// MaxPacket is the longest packet, header and end marker included: the total-length field of the
	// packet header is 16 bits (PROTOCOL.md, the header layout). It is the bound of the encoding;
	// the datagrams on the network are far smaller.
	MaxPacket = math.MaxUint16
)

// CheckLengths reports whether tlvs can be encoded by Marshal without a length wrapping: every value
// fits the 16-bit TLV length field (MaxTLVValue) and the whole packet, the header, the TLVs and the
// end marker, fits the 16-bit packet length field (MaxPacket). The error matches the module's
// ErrInvalid and names the TLV type or the packet and the sizes involved, never a value. Marshal does
// not check (it has no error result), so every caller that encodes a packet from a value it did not
// bound itself calls this first: the client's send and the fake switch's reply do.
func CheckLengths(tlvs []TLV) error {
	size := HeaderLen + len(packetEnd)
	for _, t := range tlvs {
		if len(t.Value) > MaxTLVValue {
			return invalid.Errorf("escp: the value of TLV type %d is %d bytes, above the limit of %d bytes (a TLV length is 16 bits)", t.Type, len(t.Value), MaxTLVValue)
		}
		size += TLVHeaderLen + len(t.Value)
	}
	if size > MaxPacket {
		return invalid.Errorf("escp: the packet would be %d bytes, above the limit of %d bytes (the packet length is 16 bits)", size, MaxPacket)
	}
	return nil
}
