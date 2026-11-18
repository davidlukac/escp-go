// Package inner is a package under the fake switch's directory that imports the fake switch: no finding,
// because the no-fake-in-production rule leaves the directory out.
package inner

import "github.com/davidlukac/escp-go/escptest"

// Name is the name of the fake switch.
func Name() string { return escptest.Name }
