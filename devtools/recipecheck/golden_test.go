package main

import (
	"fmt"
	"slices"
	"testing"
)

// The cases of the check, as describeDry and describeExit say them, written out: a case dropped, added or renamed needs a deliberate edit
// of these lists (a case that goes missing is otherwise noticed by nothing when another case fires for
// the same fault).
var goldenDryCases = []string{
	"mutation with the coefficient 10 on the command line | env [] | args [\"mutation\" \"TIMEOUT_COEFFICIENT=10\"] | ending \" --output build/mutation.json\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 10\"] | lacks [] | plain false",
	"mutation with the coefficient 10 in the environment | env [\"TIMEOUT_COEFFICIENT=10\"] | args [\"mutation\"] | ending \" --output build/mutation.json\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 10\"] | lacks [] | plain false",
	"mutation-diff with the coefficient 10 on the command line | env [] | args [\"mutation-diff\" \"TIMEOUT_COEFFICIENT=10\"] | ending \" --output build/mutation.json --diff origin/main\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 10\"] | lacks [] | plain false",
	"mutation-diff with the coefficient 10 in the environment | env [\"TIMEOUT_COEFFICIENT=10\"] | args [\"mutation-diff\"] | ending \" --output build/mutation.json --diff origin/main\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 10\"] | lacks [] | plain false",
	"mutation-extended with the coefficient 10 on the command line | env [] | args [\"mutation-extended\" \"TIMEOUT_COEFFICIENT=10\"] | ending \"--remove-self-assignments\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 10\" \"--output build/mutation-extended.json\"] | lacks [] | plain false",
	"mutation-extended with the coefficient 10 in the environment | env [\"TIMEOUT_COEFFICIENT=10\"] | args [\"mutation-extended\"] | ending \"--remove-self-assignments\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 10\" \"--output build/mutation-extended.json\"] | lacks [] | plain false",
	"mutation with the coefficient 20 on the command line | env [] | args [\"mutation\" \"TIMEOUT_COEFFICIENT=20\"] | ending \" --output build/mutation.json\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 20\"] | lacks [] | plain false",
	"mutation with the coefficient 20 in the environment | env [\"TIMEOUT_COEFFICIENT=20\"] | args [\"mutation\"] | ending \" --output build/mutation.json\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 20\"] | lacks [] | plain false",
	"mutation-diff with the coefficient 20 on the command line | env [] | args [\"mutation-diff\" \"TIMEOUT_COEFFICIENT=20\"] | ending \" --output build/mutation.json --diff origin/main\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 20\"] | lacks [] | plain false",
	"mutation-diff with the coefficient 20 in the environment | env [\"TIMEOUT_COEFFICIENT=20\"] | args [\"mutation-diff\"] | ending \" --output build/mutation.json --diff origin/main\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 20\"] | lacks [] | plain false",
	"mutation-extended with the coefficient 20 on the command line | env [] | args [\"mutation-extended\" \"TIMEOUT_COEFFICIENT=20\"] | ending \"--remove-self-assignments\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 20\" \"--output build/mutation-extended.json\"] | lacks [] | plain false",
	"mutation-extended with the coefficient 20 in the environment | env [\"TIMEOUT_COEFFICIENT=20\"] | args [\"mutation-extended\"] | ending \"--remove-self-assignments\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 20\" \"--output build/mutation-extended.json\"] | lacks [] | plain false",
	"mutation with EFFICACY=\"90\" | env [] | args [\"mutation\" \"EFFICACY=90\"] | ending \" --output build/mutation.json\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 10\"] | lacks [] | plain false",
	"mutation-diff with EFFICACY=\"90\" | env [] | args [\"mutation-diff\" \"EFFICACY=90\"] | ending \" --output build/mutation.json --diff origin/main\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 10\"] | lacks [] | plain false",
	"mutation with EFFICACY=\"85.5\" | env [] | args [\"mutation\" \"EFFICACY=85.5\"] | ending \" --output build/mutation.json\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 10\"] | lacks [] | plain false",
	"mutation-diff with EFFICACY=\"85.5\" | env [] | args [\"mutation-diff\" \"EFFICACY=85.5\"] | ending \" --output build/mutation.json --diff origin/main\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 10\"] | lacks [] | plain false",
	"mutation with EFFICACY=\"\" | env [] | args [\"mutation\" \"EFFICACY=\"] | ending \" --output build/mutation.json\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 10\"] | lacks [] | plain false",
	"mutation-diff with EFFICACY=\"\" | env [] | args [\"mutation-diff\" \"EFFICACY=\"] | ending \" --output build/mutation.json --diff origin/main\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 10\"] | lacks [] | plain false",
	"mutation with EFFICACY=90 in the environment | env [\"EFFICACY=90\"] | args [\"mutation\"] | ending \" --output build/mutation.json\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 10\"] | lacks [] | plain false",
	"mutation-diff with EFFICACY=90 in the environment | env [\"EFFICACY=90\"] | args [\"mutation-diff\"] | ending \" --output build/mutation.json --diff origin/main\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 10\"] | lacks [] | plain false",
	"mutation-extended with EFFICACY=90 (informational: no threshold, no check) | env [] | args [\"mutation-extended\" \"EFFICACY=90\"] | ending \"--remove-self-assignments\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 10\" \"--output build/mutation-extended.json\"] | lacks [] | plain false",
	"mutation-diff with BASE=v1.2.3 | env [] | args [\"mutation-diff\" \"BASE=v1.2.3\"] | ending \" --output build/mutation.json --diff v1.2.3\" | has [\"--timeout-coefficient 10\"] | lacks [] | plain false",
	"mutation with the default workers and no PKG | env [] | args [\"mutation\"] | ending \" --output build/mutation.json\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 10\" \"--workers 4\"] | lacks [\"-E '^safety/[^/]+$'\"] | plain false",
	"mutation with WORKERS=7 | env [] | args [\"mutation\" \"WORKERS=7\"] | ending \" --output build/mutation.json\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 10\" \"--workers 7\"] | lacks [] | plain false",
	"mutation with PKG=model | env [] | args [\"mutation\" \"PKG=model\"] | ending \" --output build/mutation.json\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 10\" \"--workers 4\" \"-E '^safety/[^/]+$'\" \"-E '^internal/udp/[^/]+$'\"] | lacks [\"-E '^model/[^/]+$'\"] | plain false",
	"mutation-diff with PKG=. | env [] | args [\"mutation-diff\" \"PKG=.\"] | ending \" --output build/mutation.json --diff origin/main\" | has [\"GOFLAGS=-count=1\" \"--timeout-coefficient 10\" \"-E '^model/[^/]+$'\"] | lacks [] | plain false",
	"recipemutants runs the mutants test with its tag and needs its PASS line | env [] | args [\"recipemutants\"] | ending \"\" | has [] | lacks [] | plain true",
	"make lint runs this check | env [] | args [\"lint\"] | ending \"\" | has [] | lacks [] | plain false",
}

var goldenExitCases = []string{
	"mutation with a good report | args [\"mutation\" \"EFFICACY=90\"] | report \"good\" | stale false | want 0",
	"mutation with a report below the threshold | args [\"mutation\" \"EFFICACY=90\"] | report \"bad\" | stale false | want 1",
	"mutation with a report below a threshold of 50 | args [\"mutation\" \"EFFICACY=50\"] | report \"bad\" | stale false | want 0",
	"mutation without a report | args [\"mutation\" \"EFFICACY=90\"] | report \"\" | stale false | want 1",
	"mutation with a stale good report | args [\"mutation\" \"EFFICACY=90\"] | report \"\" | stale true | want 1",
	"mutation with a threshold that is not a percentage | args [\"mutation\" \"EFFICACY=90%\"] | report \"good\" | stale false | want 2",
	"mutation with an empty threshold | args [\"mutation\" \"EFFICACY=\"] | report \"good\" | stale false | want 2",
	"mutation with a threshold of spaces | args [\"mutation\" \"EFFICACY= \"] | report \"good\" | stale false | want 2",
	"mutation without a threshold | args [\"mutation\"] | report \"good\" | stale false | want 0",
	"mutation with a positive coefficient | args [\"mutation\" \"TIMEOUT_COEFFICIENT=7\"] | report \"good\" | stale false | want 0",
	"mutation with the coefficient \"-1\" | args [\"mutation\" \"TIMEOUT_COEFFICIENT=-1\"] | report \"good\" | stale false | want 2",
	"mutation with the coefficient \"0\" | args [\"mutation\" \"TIMEOUT_COEFFICIENT=0\"] | report \"good\" | stale false | want 2",
	"mutation with the coefficient \"abc\" | args [\"mutation\" \"TIMEOUT_COEFFICIENT=abc\"] | report \"good\" | stale false | want 2",
	"mutation with the coefficient \"1.5\" | args [\"mutation\" \"TIMEOUT_COEFFICIENT=1.5\"] | report \"good\" | stale false | want 2",
	"mutation with the coefficient \"\" | args [\"mutation\" \"TIMEOUT_COEFFICIENT=\"] | report \"good\" | stale false | want 2",
	"mutation with the coefficient \"010\" | args [\"mutation\" \"TIMEOUT_COEFFICIENT=010\"] | report \"good\" | stale false | want 2",
	"mutation with the coefficient \"10000\" | args [\"mutation\" \"TIMEOUT_COEFFICIENT=10000\"] | report \"good\" | stale false | want 2",
	"mutation-diff with a good report | args [\"mutation-diff\" \"EFFICACY=90\"] | report \"good\" | stale false | want 0",
	"mutation-diff with a report below the threshold | args [\"mutation-diff\" \"EFFICACY=90\"] | report \"bad\" | stale false | want 1",
	"mutation-diff with a report below a threshold of 50 | args [\"mutation-diff\" \"EFFICACY=50\"] | report \"bad\" | stale false | want 0",
	"mutation-diff without a report | args [\"mutation-diff\" \"EFFICACY=90\"] | report \"\" | stale false | want 1",
	"mutation-diff with a stale good report | args [\"mutation-diff\" \"EFFICACY=90\"] | report \"\" | stale true | want 1",
	"mutation-diff with a threshold that is not a percentage | args [\"mutation-diff\" \"EFFICACY=90%\"] | report \"good\" | stale false | want 2",
	"mutation-diff with an empty threshold | args [\"mutation-diff\" \"EFFICACY=\"] | report \"good\" | stale false | want 2",
	"mutation-diff with a threshold of spaces | args [\"mutation-diff\" \"EFFICACY= \"] | report \"good\" | stale false | want 2",
	"mutation-diff without a threshold | args [\"mutation-diff\"] | report \"good\" | stale false | want 0",
	"mutation-diff with a positive coefficient | args [\"mutation-diff\" \"TIMEOUT_COEFFICIENT=7\"] | report \"good\" | stale false | want 0",
	"mutation-diff with the coefficient \"-1\" | args [\"mutation-diff\" \"TIMEOUT_COEFFICIENT=-1\"] | report \"good\" | stale false | want 2",
	"mutation-diff with the coefficient \"0\" | args [\"mutation-diff\" \"TIMEOUT_COEFFICIENT=0\"] | report \"good\" | stale false | want 2",
	"mutation-diff with the coefficient \"abc\" | args [\"mutation-diff\" \"TIMEOUT_COEFFICIENT=abc\"] | report \"good\" | stale false | want 2",
	"mutation-diff with the coefficient \"1.5\" | args [\"mutation-diff\" \"TIMEOUT_COEFFICIENT=1.5\"] | report \"good\" | stale false | want 2",
	"mutation-diff with the coefficient \"\" | args [\"mutation-diff\" \"TIMEOUT_COEFFICIENT=\"] | report \"good\" | stale false | want 2",
	"mutation-diff with the coefficient \"010\" | args [\"mutation-diff\" \"TIMEOUT_COEFFICIENT=010\"] | report \"good\" | stale false | want 2",
	"mutation-diff with the coefficient \"10000\" | args [\"mutation-diff\" \"TIMEOUT_COEFFICIENT=10000\"] | report \"good\" | stale false | want 2",
	"mutation-extended with the coefficient \"-1\" | args [\"mutation-extended\" \"TIMEOUT_COEFFICIENT=-1\"] | report \"\" | stale false | want 2",
	"mutation-extended with the coefficient \"0\" | args [\"mutation-extended\" \"TIMEOUT_COEFFICIENT=0\"] | report \"\" | stale false | want 2",
	"mutation-extended with the coefficient \"abc\" | args [\"mutation-extended\" \"TIMEOUT_COEFFICIENT=abc\"] | report \"\" | stale false | want 2",
	"mutation-extended with the coefficient \"1.5\" | args [\"mutation-extended\" \"TIMEOUT_COEFFICIENT=1.5\"] | report \"\" | stale false | want 2",
	"mutation-extended with the coefficient \"\" | args [\"mutation-extended\" \"TIMEOUT_COEFFICIENT=\"] | report \"\" | stale false | want 2",
	"mutation-extended with the coefficient \"010\" | args [\"mutation-extended\" \"TIMEOUT_COEFFICIENT=010\"] | report \"\" | stale false | want 2",
	"mutation-extended with the coefficient \"10000\" | args [\"mutation-extended\" \"TIMEOUT_COEFFICIENT=10000\"] | report \"\" | stale false | want 2",
	"mutation-extended with the default coefficient | args [\"mutation-extended\"] | report \"\" | stale false | want 0",
}

// describeDry and describeExit say what a case is, not only its label: the environment, the arguments and
// the pins of a recipe case, and the arguments, the report and the expectation of an exit-code case.
func describeDry(tc dryCase) string {
	return fmt.Sprintf("%s | env %q | args %q | ending %q | has %q | lacks %q | plain %v", tc.label, tc.env, tc.args, tc.pins.ending, tc.pins.has, tc.pins.lacks, tc.plain)
}

func describeExit(tc exitCase) string {
	return fmt.Sprintf("%s | args %q | report %q | stale %v | want %d", tc.label, tc.args, tc.report, tc.stale, tc.want)
}

func TestTheCasesAreExactlyTheGoldenLists(t *testing.T) {
	var dry, exit []string
	for _, tc := range dryCases() {
		dry = append(dry, describeDry(tc))
	}
	for _, tc := range exitCases() {
		exit = append(exit, describeExit(tc))
	}
	if !slices.Equal(dry, goldenDryCases) {
		t.Errorf("the recipe cases are\n%q\nwant\n%q", dry, goldenDryCases)
	}
	if !slices.Equal(exit, goldenExitCases) {
		t.Errorf("the exit-code cases are\n%q\nwant\n%q", exit, goldenExitCases)
	}
}
