// Command mergereadiness is the check behind the "Merge readiness" job: it reads the description of
// a pull request from the file named by -body-file (at most 1 MiB), prints which boxes of its "Merge
// readiness" section are not ticked and exits 1 when there are any, or when the markup is ambiguous, or
// when the section has no boxes it reads. A description without the section passes with a message. The
// rules and the reasons are in the documentation of package readiness; it is a build tool of this
// repository, not part of the API.
//
//	go run ./internal/mergereadiness -body-file description.md
package main

import (
	"os"

	"github.com/davidlukac/escp-go/internal/mergereadiness/readiness"
)

// main is the process entry point.
//
// coverage: ignore - process entry point; readiness.Run holds all the logic and is tested.
func main() { os.Exit(readiness.Run(os.Args[1:], os.Stdout)) }
