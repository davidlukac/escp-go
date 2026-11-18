#!/bin/sh
# Pins the recipes of `make mutation`, `make mutation-diff` and `make mutation-extended` as `make -n`
# prints them, without running anything. Every line of a recipe is compared whole (the line of gremlins
# aside, see below), so a substring cannot satisfy it: `check ... || true`, a `-` prefix that makes a
# failing step non-fatal, `rm -f build/mutation.jsonx`, a hardcoded threshold, a duplicated or extra
# step, a step in the wrong order all fail.
#
#   * the old report is deleted first (gremlins exits 0 and writes no report when it has nothing to
#     report, so a stale one would be judged in place of the run);
#   * with EFFICACY set to anything, an explicitly empty value or one of spaces included, the threshold
#     is validated first and the efficacy check runs after gremlins, with the value given (two values
#     are tried, so a hardcoded one fails); without it no check runs (the origin of the variable is
#     what counts, not its value);
#   * the timeout coefficient is validated before gremlins, in every target, with the value given;
#   * the line of gremlins itself (its exclusions and its PKG filter vary) must start with
#     `GOFLAGS=-count=1` (a cached baseline makes every mutant time out, and a timeout counts as
#     killed), hand gremlins the coefficient (10 by default; overridable on the command line and in the
#     environment) and the report path it is checked at, and never `--threshold-efficacy` (the check
#     counts a timed-out mutant as killed, gremlins does not);
#   * the exit codes are run, too, with gremlins stood in for (a `-` prefix on a step is invisible to
#     `make -n`); the report of the working tree is put aside and back;
#   * `make lint` runs this script.
#
# Run by `make lint` (the `mutationrecipe` target). Needs make and a POSIX shell, nothing else.
set -eu
cd "$(dirname "$0")/.."

# mk ARGUMENT...: make, as a fresh run: GNU make 4 prints "Entering directory" lines when it runs
# inside another make (`make lint`), and the command-line variables of the outer one travel in MAKEFLAGS.
mk() {
	env -u MAKEFLAGS -u MFLAGS -u MAKELEVEL "${MAKE:-make}" --no-print-directory "$@"
}
out=$(mktemp)
want=$(mktemp)
got=$(mktemp)
gremlins=$(mktemp)
trap 'rm -f "$out" "$want" "$got" "$gremlins"' EXIT
# A signal ends the script through its EXIT trap too (a `timeout` wrapper sends TERM to this pid only):
# the working tree's report is restored and the temporary files removed.
trap 'exit 1' INT TERM HUP
good_report=
bad_report=
standin=
standin_log=
failed=0

fail() {
	echo "check-mutation-recipe: $*" >&2
	failed=1
}

check=./internal/mutationreport
report=build/mutation.json

# expect LABEL LINE...: the recipe that `make -n` printed into $out, with the line of gremlins turned
# into GREMLINS (and kept apart in $gremlins), is exactly these lines, in this order.
expect() {
	label=$1
	shift
	grep -F -e 'gremlins unleash' "$out" >"$gremlins" || true
	if [ "$(wc -l <"$gremlins" | tr -d ' ')" -ne 1 ]; then
		fail "$label: the recipe does not run gremlins exactly once"
	fi
	sed 's/^GOFLAGS=-count=1 go tool -modfile=devtools\/go.mod gremlins unleash \..*$/GREMLINS/' "$out" >"$got"
	: >"$want"
	for line in "$@"; do
		printf '%s\n' "$line" >>"$want"
	done
	if ! diff "$want" "$got" >/dev/null; then
		fail "$label: the recipe is not what it must be; wanted, then got:"
		cat "$want" "$got" >&2
	fi
}

# gremlins_has LABEL ENDING FRAGMENT...: the line of gremlins ends with ENDING and holds each fragment
# (with a space on each side), and never --threshold-efficacy.
gremlins_has() {
	label=$1
	ending=$2
	shift 2
	line=$(cat "$gremlins")
	case "$line" in
	*"$ending") ;;
	*) fail "$label: gremlins is not run ending with '$ending'" ;;
	esac
	for fragment in "$@"; do
		case " $line " in
		*" $fragment "*) ;;
		*) fail "$label: gremlins is not run with '$fragment'" ;;
		esac
	done
	case "$line" in
	*--threshold-efficacy*) fail "$label: gremlins is handed --threshold-efficacy" ;;
	esac
}

# recipe_of ARGUMENT...: what `make -n` prints for the arguments, into $out.
recipe_of() {
	mk -n "$@" >"$out"
}

for coefficient in 10 20; do
	# EFFICACY unset: no validation of the threshold and no check.
	for how in command environment; do
		for target in mutation mutation-diff mutation-extended; do
			label="$target (coefficient $coefficient by $how)"
			if [ "$how" = command ]; then
				recipe_of "$target" TIMEOUT_COEFFICIENT=$coefficient
			else
				(TIMEOUT_COEFFICIENT=$coefficient && export TIMEOUT_COEFFICIENT && mk -n "$target" >"$out")
			fi
			case $target in
			mutation)
				expect "$label" 'mkdir -p build' "rm -f $report" "go run $check coefficient \"$coefficient\"" GREMLINS
				;;
			mutation-diff)
				expect "$label" 'mkdir -p build' "rm -f $report" "go run $check coefficient \"$coefficient\"" GREMLINS
				;;
			mutation-extended)
				expect "$label" 'mkdir -p build' "go run $check coefficient \"$coefficient\"" GREMLINS
				;;
			esac
			case $target in
			mutation) gremlins_has "$label" " --output $report" "--timeout-coefficient $coefficient" "GOFLAGS=-count=1" ;;
			mutation-diff) gremlins_has "$label" " --output $report --diff origin/main" "--timeout-coefficient $coefficient" "GOFLAGS=-count=1" ;;
			mutation-extended) gremlins_has "$label" "--remove-self-assignments" "--output build/mutation-extended.json" "--timeout-coefficient $coefficient" "GOFLAGS=-count=1" ;;
			esac
		done
	done
done

# EFFICACY set: to two values (so that a hardcoded one fails), empty and of spaces (an error, never a
# skip), and from the environment (the workflow's way is the command line).
# efficacy_case GIVEN SEEN: EFFICACY=GIVEN on the command line; make hands the recipe SEEN. A value of
# spaces is only tried by its exit code below: make 3.81 strips it to nothing and make 4 keeps it.
efficacy_case() {
	value=$2
	for target in mutation mutation-diff; do
		label="$target with EFFICACY='$1'"
		recipe_of "$target" EFFICACY="$1"
		expect "$label" 'mkdir -p build' "rm -f $report" "go run $check threshold \"$value\"" "go run $check coefficient \"10\"" GREMLINS "go run $check check $report \"$value\""
		case $target in
		mutation) gremlins_has "$label" " --output $report" "--timeout-coefficient 10" "GOFLAGS=-count=1" ;;
		mutation-diff) gremlins_has "$label" " --output $report --diff origin/main" "--timeout-coefficient 10" "GOFLAGS=-count=1" ;;
		esac
	done
}
efficacy_case 90 90
efficacy_case 85.5 85.5
efficacy_case '' ''

(EFFICACY=90 && export EFFICACY && mk -n mutation >"$out")
expect "mutation with EFFICACY=90 in the environment" 'mkdir -p build' "rm -f $report" "go run $check threshold \"90\"" "go run $check coefficient \"10\"" GREMLINS "go run $check check $report \"90\""

# the extended run is informational: never a threshold, never a check, whatever EFFICACY says.
mk -n mutation-extended EFFICACY=90 >"$out"
expect "mutation-extended with EFFICACY=90" 'mkdir -p build' "go run $check coefficient \"10\"" GREMLINS

# --diff takes BASE.
mk -n mutation-diff BASE=v1.2.3 >"$out"
grep -F -e 'gremlins unleash' "$out" >"$gremlins" || true
gremlins_has "mutation-diff with BASE" " --output $report --diff v1.2.3" "--timeout-coefficient 10"

# `make -n` hides a `-` prefix (a failing step that does not fail make), so the exit codes are run too,
# with gremlins replaced by a stand-in (MUTATE): a report that is missing, stale, bad or good, a bad
# threshold, a bad coefficient. The report of the working tree is put aside and back.
saved=
if [ -e "$report" ]; then
	saved=$(mktemp)
	cp -p "$report" "$saved"
fi
# shellcheck disable=SC2329 # run by the trap
restore() {
	rm -f "$out" "$want" "$got" "$gremlins" "$good_report" "$bad_report" "$standin" "$standin_log"
	if [ -n "$saved" ]; then
		mkdir -p build
		cp -p "$saved" "$report"
		rm -f "$saved"
	else
		rm -f "$report"
	fi
}
good_report=$(mktemp)
bad_report=$(mktemp)
standin=$(mktemp)
trap restore EXIT
# The stand-in for gremlins (it is given gremlins' arguments and ignores them): writes the report that
# STANDIN_REPORT names, as a finished run does, or nothing when it is empty, and leaves a mark in
# STANDIN_LOG that it ran.
# shellcheck disable=SC2016 # the stand-in expands it, not this script
printf '%s\n' 'echo ran >>"$STANDIN_LOG"' '[ -z "$STANDIN_REPORT" ] || cp "$STANDIN_REPORT" build/mutation.json' >"$standin"
standin_log=$(mktemp)
STANDIN_LOG=$standin_log
export STANDIN_LOG
use_report() {
	STANDIN_REPORT=$1
	export STANDIN_REPORT
}
printf '%s' '{"files":[{"file_name":"a.go","mutations":[{"type":"X","status":"KILLED","line":1,"column":1},{"type":"X","status":"TIMED OUT","line":2,"column":1}]}]}' >"$good_report"
printf '%s' '{"files":[{"file_name":"a.go","mutations":[{"type":"X","status":"KILLED","line":1,"column":1},{"type":"X","status":"LIVED","line":2,"column":1}]}]}' >"$bad_report"
mkdir -p build

# exits WANT LABEL ARGUMENT...: make with the arguments exits zero (WANT=0) or not (WANT=1). The
# runs with a stand-in also set GREMLINS=false, so that a recipe which stops using the stand-in hook
# (MUTATE, MUTATE_EXTENDED) and expands $(GREMLINS) fails at once and never starts the real tool.
exits() {
	want_zero=$1
	label=$2
	shift 2
	if mk "$@" GREMLINS=false >/dev/null 2>&1; then
		[ "$want_zero" = 0 ] || fail "$label: make succeeded and must fail"
	else
		[ "$want_zero" = 1 ] || fail "$label: make failed and must succeed"
	fi
}

# refuses LABEL ARGUMENT...: make fails and gremlins was never started (the validations come first).
refuses() {
	label=$1
	shift
	: >"$standin_log"
	if mk "$@" GREMLINS=false >/dev/null 2>&1; then
		fail "$label: make succeeded and must fail"
	elif [ -s "$standin_log" ]; then
		fail "$label: make failed, but only after gremlins was started"
	fi
}

for target in mutation mutation-diff; do
	use_report "$good_report"
	exits 0 "$target with a good report" "$target" EFFICACY=90 MUTATE="sh $standin"
	use_report "$bad_report"
	exits 1 "$target with a report below the threshold" "$target" EFFICACY=90 MUTATE="sh $standin"
	exits 0 "$target with a report below a threshold of 50" "$target" EFFICACY=50 MUTATE="sh $standin"
	use_report ''
	exits 1 "$target without a report" "$target" EFFICACY=90 MUTATE="sh $standin"
	cp "$good_report" "$report"
	exits 1 "$target with a stale good report" "$target" EFFICACY=90 MUTATE="sh $standin"
	use_report "$good_report"
	refuses "$target with a bad threshold" "$target" EFFICACY=90% MUTATE="sh $standin"
	refuses "$target with an empty threshold" "$target" EFFICACY= MUTATE="sh $standin"
	refuses "$target with a threshold of spaces" "$target" EFFICACY=" " MUTATE="sh $standin"
	exits 0 "$target without a threshold" "$target" MUTATE="sh $standin"
	for coefficient in -1 0 abc 1.5 ''; do
		refuses "$target with the coefficient '$coefficient'" "$target" TIMEOUT_COEFFICIENT="$coefficient" MUTATE="sh $standin"
	done
	exits 0 "$target with a positive coefficient" "$target" TIMEOUT_COEFFICIENT=7 MUTATE="sh $standin"
done
for coefficient in -1 0 abc 1.5 ''; do
	refuses "mutation-extended with the coefficient '$coefficient'" mutation-extended TIMEOUT_COEFFICIENT="$coefficient" MUTATE_EXTENDED="sh $standin"
done
exits 0 "mutation-extended" mutation-extended MUTATE_EXTENDED=true

# --workers (WORKERS, default 4) and the PKG filter are part of the gremlins line.
recipe_of mutation
grep -F -e 'gremlins unleash' "$out" >"$gremlins" || true
gremlins_has "mutation default workers" " --output $report" "--workers 4"
case " $(cat "$gremlins") " in
*"^safety/[^/]+\$'"*) fail "mutation without PKG: the line excludes the directories of a PKG filter" ;;
esac
recipe_of mutation WORKERS=7
grep -F -e 'gremlins unleash' "$out" >"$gremlins" || true
gremlins_has "mutation with WORKERS=7" " --output $report" "--workers 7"
recipe_of mutation PKG=model
grep -F -e 'gremlins unleash' "$out" >"$gremlins" || true
gremlins_has "mutation with PKG=model" " --output $report" "-E '^safety/[^/]+\$'" "-E '^internal/udp/[^/]+\$'"
case " $(cat "$gremlins") " in
*"-E '^model/[^/]+\$'"*) fail "mutation with PKG=model: the filter excludes model itself" ;;
esac
recipe_of mutation-diff PKG=.
grep -F -e 'gremlins unleash' "$out" >"$gremlins" || true
gremlins_has "mutation-diff with PKG=." " --output $report --diff origin/main" "-E '^model/[^/]+\$'"

# the recipe is part of what `make lint` runs.
mk -n lint >"$out"
grep -q -x -F 'scripts/check-mutation-recipe.sh' "$out" || fail "make lint does not run this script"

exit "$failed"
