package model

import (
	"github.com/davidlukac/escp-go/internal/invalid"
)

// ErrInvalid means the caller's argument was refused before any write was sent: a port or
// id out of range, a rate that is not a multiple of the step, a combination the switch
// cannot keep. The message names the value and what is allowed; test for the class with
// errors.Is(err, ErrInvalid). It is never returned for a reply the client cannot parse,
// nor for a failure of the switch.
var ErrInvalid = invalid.Err
