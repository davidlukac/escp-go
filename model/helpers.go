package model

import (
	"github.com/davidlukac/escp-go/internal/invalid"
	"github.com/davidlukac/escp-go/internal/wire"
)

// invalidf builds an error that matches ErrInvalid and reads as the formatted message.
func invalidf(format string, args ...any) error {
	return invalid.Errorf(format, args...)
}

// maskLen is the size of a port mask on the wire.
const maskLen = wire.MaskLen

// macLen is the length of an Ethernet MAC address in bytes.
const macLen = wire.MACLen
