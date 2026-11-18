package safety

import "github.com/davidlukac/escp-go/internal/wire"

// ReachDown imports an internal package that the safety layer may not: the safety-layer rule refuses it.
func ReachDown() string { return wire.Name }
