// Command zizmorverdict is the verdict of `make zizmor`: it reads zizmor's answer (--format json-v1) on its
// standard input and the number of findings of the `self-repository` rule the workflows may carry as its
// one argument, and exits 0 only when the answer holds exactly that many findings of that rule and
// no other finding. Anything it does not understand (an answer that is not a list, a finding without a rule,
// trailing text, an expected count that is not a plain non-negative integer) is a failure. The rules and the
// reasons are in the documentation of package verdict; it is a build tool of this repository, not part of
// the API, run by scripts/zizmor.sh.
//
//	zizmor ... --format json-v1 | go run ./internal/zizmorverdict 13
package main

import (
	"os"

	"github.com/davidlukac/escp-go/internal/zizmorverdict/verdict"
)

// main is the process entry point.
//
// coverage: ignore - process entry point; verdict.Run holds all the logic and is tested.
func main() { os.Exit(verdict.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }
