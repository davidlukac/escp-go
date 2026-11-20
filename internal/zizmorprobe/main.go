// Command zizmorprobe is the check behind `make zizmorcheck`: it proves that scripts/zizmor.sh (what `make
// zizmor` runs) reports a deliberate finding whatever the audited tree says about the check itself. The
// cases, the fixtures and the reasons are in the documentation of package probe; it is a build tool of this
// repository, not part of the API.
//
//	ZIZMOR_SELF_REPOSITORY_FINDINGS=13 go run ./internal/zizmorprobe
//
// The program is also the stand-in for zizmor in the probes that record the command line and the
// environment of the call: it behaves so when it is run under the name zizmor.
package main

import (
	"context"
	"os"

	"github.com/davidlukac/escp-go/internal/zizmorprobe/probe"
)

// main is the process entry point.
//
// coverage: ignore - process entry point; probe.Run holds all the logic and is tested.
func main() {
	os.Exit(probe.Run(context.Background(), os.Args, probe.ProcessEnvironment()))
}
