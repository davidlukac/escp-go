package escp

import "github.com/davidlukac/escp-go/escptest"

// UseTheFake depends on the fake switch from production code, which the no-fake-in-production rule refuses.
func UseTheFake() string { return escptest.Name }
