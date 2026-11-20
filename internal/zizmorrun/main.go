// Command zizmorrun is what `make zizmor` runs (through scripts/zizmor.sh): it audits a repository with the
// pinned zizmor, with fixed options and a cleaned environment so that nothing in the audited tree changes
// what is audited, and judges the answer against an exact expectation. The options, the variables that are
// cleared and the reasons are in the documentation of package audit; it is a build tool of this repository,
// not part of the API.
//
//	go run ./internal/zizmorrun ROOT EXPECTED
//
// ROOT is the repository to audit and EXPECTED the number of `self-repository` findings it may carry.
package main

import (
	"context"
	"os"

	"github.com/davidlukac/escp-go/internal/zizmorrun/audit"
)

// main is the process entry point.
//
// coverage: ignore - process entry point; audit.Run holds all the logic and is tested.
func main() {
	os.Exit(audit.Run(context.Background(), os.Args[1:], audit.ProcessEnvironment()))
}
