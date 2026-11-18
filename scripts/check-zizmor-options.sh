#!/bin/sh
# Proves that scripts/zizmor.sh (what `make zizmor` runs) reports a deliberate finding whatever the
# checked-out tree says about the check itself. zizmor reads inputs from the tree it audits: a
# configuration file, an ignore file, an inline suppression comment, and the files it can load. For each of those inputs this
# script copies the repository's .github/ into a scratch git repository, adds a workflow with a
# template injection (the finding) and the input, runs scripts/zizmor.sh offline, and requires a
# failing exit with the finding (or the allow-list message) in the output. Two controls come first:
# the copy alone is clean, and the finding alone is reported, so a probe that passes cannot be a
# probe that never ran.
#
#   scripts/check-zizmor-options.sh
#
# It needs zizmor on the PATH, the pinned version (`make zizmor` prints how to install it). Without
# it the script says so and succeeds, unless ZIZMOR_REQUIRED is set, as it is in the CI jobs that
# install zizmor. ZIZMOR_SELF_REPOSITORY_IGNORES is the number the Makefile holds; it needs no
# network: the audits run offline.
set -eu

repo=$(cd "$(dirname "$0")/.." && pwd)
: "${ZIZMOR_SELF_REPOSITORY_IGNORES:?set it, as the Makefile does}"

if ! command -v zizmor >/dev/null 2>&1; then
	if [ -n "${ZIZMOR_REQUIRED:-}" ]; then
		echo "check-zizmor-options: zizmor is not installed and ZIZMOR_REQUIRED is set" >&2
		exit 1
	fi
	echo "check-zizmor-options: skipped, zizmor is not installed (CI installs the pinned one; see make zizmor)"
	exit 0
fi

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT

# The workflow with the finding, and the name zizmor reports for it.
finding_file=.github/workflows/zz-probe.yml
finding_rule=template-injection
add_finding() {
	cat >"$1/$finding_file" <<'YAML'
name: Probe
on: pull_request
permissions: {}
jobs:
  probe:
    runs-on: ubuntu-24.04
    permissions: {}
    steps:
      - run: echo "${{ github.head_ref }}"
YAML
}

# Each input is added by a function of the scratch directory.
disable_rule_config() {
	cat >"$1/$2" <<YAML
rules:
  $finding_rule:
    disable: true
YAML
}
ignore_the_finding_file() {
	echo "zz-probe.yml" >"$1/$2"
}
# Variants of the finding workflow that zizmor cannot load: each must fail the run, not be skipped.
break_with_bom() {
	{ printf '\357\273\277'; cat "$1/$finding_file"; } >"$1/$finding_file.new"
	mv "$1/$finding_file.new" "$1/$finding_file"
}
break_with_second_document() {
	{ cat "$1/$finding_file"; echo '---'; cat "$1/$finding_file"; } >"$1/$finding_file.new"
	mv "$1/$finding_file.new" "$1/$finding_file"
}
break_with_duplicate_key() {
	echo 'permissions: {}' >>"$1/$finding_file"
}
break_with_schema_error() {
	sed 's/ubuntu-24.04/5/' "$1/$finding_file" >"$1/$finding_file.new"
	mv "$1/$finding_file.new" "$1/$finding_file"
}
suppress_inline() {
	sed 's/echo "\${{ github.head_ref }}"/& # zizmor: ignore[template-injection] probe/' "$1/$finding_file" >"$1/$finding_file.new"
	mv "$1/$finding_file.new" "$1/$finding_file"
}

failures=0
sequence=0

# fresh DIR: a scratch git repository holding a copy of this repository's .github/.
fresh() {
	sequence=$((sequence + 1))
	dir="$scratch/$sequence"
	mkdir "$dir"
	cp -R "$repo/.github" "$dir/.github"
	git init -q "$dir"
}

# run DIR: scripts/zizmor.sh over the scratch repository, offline, output in $scratch/out, status in $status.
run() {
	status=0
	env -u GH_TOKEN -u GITHUB_TOKEN "$repo/scripts/zizmor.sh" "$1" >"$scratch/out" 2>&1 || status=$?
}

# expect_fail NAME MARKER: the last run failed and said MARKER.
expect_fail() {
	if [ "$status" -ne 0 ] && grep -q "$2" "$scratch/out"; then
		echo "ok    $1"
		return
	fi
	echo "FAIL  $1: exit $status, wanted a failure that mentions $2"
	sed 's/^/        /' "$scratch/out"
	failures=$((failures + 1))
}

fresh
run "$dir"
if [ "$status" -ne 0 ]; then
	echo "FAIL  control: the repository's own .github/ is not clean"
	sed 's/^/        /' "$scratch/out"
	exit 1
fi
echo "ok    control: the copy of .github/ alone is clean"

fresh
add_finding "$dir"
run "$dir"
expect_fail "control: the finding alone is reported" "$finding_rule"

for name in zizmor.yml zizmor.yaml .zizmor.yml .zizmor.yaml .github/zizmor.yml .github/zizmor.yaml .github/.zizmor.yml .github/.zizmor.yaml; do
	fresh
	add_finding "$dir"
	disable_rule_config "$dir" "$name"
	run "$dir"
	expect_fail "configuration file $name" "$finding_rule"
done

for name in .gitignore .ignore .github/.gitignore .github/.ignore .github/workflows/.gitignore .github/workflows/.ignore; do
	fresh
	add_finding "$dir"
	ignore_the_finding_file "$dir" "$name"
	run "$dir"
	expect_fail "ignore file $name" "$finding_rule"
done

for variant in bom second_document duplicate_key schema_error; do
	fresh
	add_finding "$dir"
	"break_with_$variant" "$dir"
	run "$dir"
	expect_fail "workflow zizmor cannot load ($variant)" "failed to load"
done

fresh
add_finding "$dir"
suppress_inline "$dir"
run "$dir"
expect_fail "an extra inline suppression" "zizmor ignore comments"

if [ "$failures" -ne 0 ]; then
	echo "check-zizmor-options: $failures probe(s) failed" >&2
	exit 1
fi
echo "check-zizmor-options: every input the tree can carry is ignored or refused"
