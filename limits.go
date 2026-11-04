package escp

import (
	"encoding/binary"

	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
)

// maskLen is the size of a port mask on the wire.
const maskLen = wire.MaskLen

// maskAt reads the port mask at offset off of an entry whose length the caller has already checked
// against its layout; it panics, like any slice index, on input that was not checked.
func maskAt(v []byte, off int) model.PortSet {
	return model.PortSet(binary.BigEndian.Uint32(v[off : off+maskLen]))
}
