#!/bin/sh
# Checks the recipes of `make mutation` and `make mutation-diff` as `make -n` prints them, without
# running anything: the old report is deleted before gremlins runs (gremlins exits 0 and writes no
# report when it has nothing to report, so a stale one would be judged), and with EFFICACY set the
# threshold is validated first and the efficacy check runs after gremlins; without it no check runs and
# gremlins is never handed --threshold-efficacy (the check counts a timed-out mutant as killed); every
# gremlins run gets --timeout-coefficient 10, so that a timeout means a hang and not a slow runner.
# Run by `make lint` (the `mutationrecipe` target). Needs make and a POSIX shell, nothing else.
set -eu
cd "$(dirname "$0")/.."

out=$(mktemp)
trap 'rm -f "$out"' EXIT
failed=0

fail() {
	echo "check-mutation-recipe: $*" >&2
	failed=1
}

# line_of PATTERN: the number of the first line of $out that contains PATTERN (fixed string), or 0.
line_of() {
	n=$(grep -n -F -e "$1" "$out" | head -n 1 | cut -d: -f1)
	echo "${n:-0}"
}

# expect_order TARGET PATTERN...: each pattern is in the recipe and after the one before it.
expect_order() {
	label=$1
	shift
	previous=0
	for pattern in "$@"; do
		at=$(line_of "$pattern")
		if [ "$at" -eq 0 ]; then
			fail "$label: the recipe has no '$pattern'"
		elif [ "$at" -le "$previous" ]; then
			fail "$label: '$pattern' is not after the step before it"
		else
			previous=$at
		fi
	done
}

# Every gremlins run is handed the timeout coefficient (10 by default, overridable), the extended one too.
for target in mutation mutation-diff mutation-extended; do
	"${MAKE:-make}" -n "$target" >"$out"
	grep -F -e 'gremlins unleash' "$out" | grep -q -F -e '--timeout-coefficient 10 ' || fail "$target: gremlins is not run with --timeout-coefficient 10"
	"${MAKE:-make}" -n "$target" TIMEOUT_COEFFICIENT=20 >"$out"
	grep -F -e 'gremlins unleash' "$out" | grep -q -F -e '--timeout-coefficient 20 ' || fail "$target: TIMEOUT_COEFFICIENT does not override the coefficient"
done

for target in mutation mutation-diff; do
	"${MAKE:-make}" -n "$target" EFFICACY= >"$out"
	expect_order "$target without EFFICACY" 'rm -f build/mutation.json' 'gremlins unleash'
	[ "$(line_of 'go run ./internal/mutationreport')" -eq 0 ] || fail "$target without EFFICACY: runs the check"
	[ "$(line_of '--threshold-efficacy')" -eq 0 ] || fail "$target: hands gremlins --threshold-efficacy"

	"${MAKE:-make}" -n "$target" EFFICACY=90 >"$out"
	expect_order "$target with EFFICACY=90" \
		'rm -f build/mutation.json' \
		'go run ./internal/mutationreport threshold 90' \
		'gremlins unleash' \
		'go run ./internal/mutationreport check build/mutation.json 90'
	[ "$(line_of '--threshold-efficacy')" -eq 0 ] || fail "$target with EFFICACY=90: hands gremlins --threshold-efficacy"
done

"${MAKE:-make}" -n mutation-diff EFFICACY=90 BASE=origin/main >"$out"
grep -F -e 'gremlins unleash' "$out" | grep -q -F -e '--diff origin/main' || fail "mutation-diff: gremlins is not run with --diff origin/main"

exit "$failed"
