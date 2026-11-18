#!/bin/sh
# Proves that scripts/zizmor.sh (what `make zizmor` runs) reports a deliberate finding whatever the
# checked-out tree says about the check itself. zizmor reads inputs from the tree it audits: a
# configuration file, an ignore file, an inline suppression comment, the files it can load, and the
# files that links lead to, and where the files are. For each of those inputs this script copies the
# repository's .github/ into a scratch git repository, adds a workflow with a template injection (the finding) and the
# input, runs scripts/zizmor.sh offline, and requires a failing exit with the finding (or the
# reason the script gives) in the output. Two controls come first: the copy alone is clean, and the
# finding alone is reported, so a probe that passes cannot be a probe that never ran. The
# probes on the allowed findings (one fewer, one more) stand next to the one that shows an ignore
# comment is not read.
#
#   scripts/check-zizmor-options.sh
#
# It needs zizmor on the PATH, the pinned version (`make zizmor` prints how to install it). Without
# it the script says so and succeeds, unless ZIZMOR_REQUIRED is set, as it is in the CI jobs that
# install zizmor. ZIZMOR_SELF_REPOSITORY_FINDINGS is the number the Makefile holds; it needs no
# network: the audits run offline.
set -eu

repo=$(cd "$(dirname "$0")/.." && pwd)
: "${ZIZMOR_SELF_REPOSITORY_FINDINGS:?set it, as the Makefile does}"

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
suppress_inline_in() {
	sed 's/echo "\${{ github.head_ref }}"/& # zizmor: ignore[template-injection] probe/' "$1" >"$1.new"
	mv "$1.new" "$1"
}
suppress_inline() {
	suppress_inline_in "$1/$finding_file"
}

# The allowed findings are the uses of the local setup action; these change them one way or the other.
setup_use='uses: ./.github/actions/setup'
remove_one_setup_use() {
	grep -v "$setup_use" "$1/.github/workflows/fuzz.yml" >"$1/new.yml"
	mv "$1/new.yml" "$1/.github/workflows/fuzz.yml"
}
add_one_setup_use() {
	cat >"$1/.github/workflows/zz-extra.yml" <<YAML
name: Extra
on: pull_request
permissions: {}
jobs:
  extra:
    runs-on: ubuntu-24.04
    permissions: {}
    steps:
      - $setup_use
YAML
}
name_another_rule_on_a_setup_use() {
	sed 's/ignore\[self-repository\]/ignore[template-injection]/' "$1/.github/workflows/fuzz.yml" >"$1/new.yml"
	mv "$1/new.yml" "$1/.github/workflows/fuzz.yml"
}
# Links to a file outside .github/ whose content carries an ignore comment for the finding.
link_a_workflow() {
	mkdir "$1/elsewhere"
	add_finding "$1"
	mv "$1/$finding_file" "$1/elsewhere/zz.yml"
	suppress_inline_in "$1/elsewhere/zz.yml"
	ln -s ../../elsewhere/zz.yml "$1/.github/workflows/zz.yml"
}
# write_action FILE: a composite action with the same template injection.
write_action() {
	cat >"$1" <<'YAML'
name: Probe
description: Probe
runs:
  using: composite
  steps:
    - shell: bash
      run: echo "${{ github.head_ref }}"
YAML
}
# A composite action outside .github/, which a workflow can use with `uses: ./ci`, and one behind a link.
add_action_outside_github() {
	mkdir "$1/ci"
	write_action "$1/ci/action.yml"
}
link_a_directory() {
	add_action_outside_github "$1"
	ln -s ../../ci "$1/.github/actions/linked"
}
# write_clean_workflow FILE: a workflow without a finding, for a link whose target is fine in itself.
write_clean_workflow() {
	cat >"$1" <<'YAML'
name: Clean
on: pull_request
permissions: {}
jobs:
  clean:
    runs-on: ubuntu-24.04
    permissions: {}
    steps:
      - run: echo clean
YAML
}
link_a_clean_workflow() {
	mkdir "$1/elsewhere"
	write_clean_workflow "$1/elsewhere/clean.yml"
	ln -s ../../elsewhere/clean.yml "$1/.github/workflows/clean.yml"
}
link_an_action() {
	mkdir "$1/elsewhere" "$1/.github/actions/linked"
	write_action "$1/elsewhere/action.yml"
	ln -s ../../../elsewhere/action.yml "$1/.github/actions/linked/action.yml"
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

# expect_pass NAME: the last run passed.
expect_pass() {
	if [ "$status" -eq 0 ]; then
		echo "ok    $1"
		return
	fi
	echo "FAIL  $1: exit $status, wanted a pass"
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
expect_fail "an extra inline suppression" "zizmor findings"

fresh
link_a_workflow "$dir"
run "$dir"
expect_fail "a symbolic link to a workflow" "symbolic links"

fresh
link_a_clean_workflow "$dir"
run "$dir"
expect_fail "a symbolic link to a workflow without a finding" "symbolic links"

fresh
link_an_action "$dir"
run "$dir"
expect_fail "a symbolic link to an action" "symbolic links"

fresh
add_action_outside_github "$dir"
run "$dir"
expect_fail "a composite action outside .github/" "$finding_rule"

fresh
link_a_directory "$dir"
run "$dir"
expect_fail "a composite action behind a linked directory" "symbolic links"

fresh
remove_one_setup_use "$dir"
run "$dir"
expect_fail "one allowed finding fewer" "zizmor findings"

fresh
add_one_setup_use "$dir"
run "$dir"
expect_fail "one allowed finding more" "zizmor findings"

fresh
remove_one_setup_use "$dir"
add_finding "$dir"
suppress_inline "$dir"
run "$dir"
expect_fail "one allowed finding fewer and a suppressed one more" "zizmor findings"

fresh
name_another_rule_on_a_setup_use "$dir"
run "$dir"
expect_pass "the rule named in an ignore comment is not read: the findings are what counts"

# The command line and the environment of the call: a stub zizmor records its arguments and
# environment, and scripts/zizmor.sh is run with every variable that could change the audit set, with
# a token (GH_TOKEN, or GITHUB_TOKEN alone) and without (unset, or set and empty). The stub answers
# an empty list of findings, so the script is run with an expectation of none.
stubs="$scratch/stub"
mkdir "$stubs"
cat >"$stubs/zizmor" <<'SH'
#!/bin/sh
printf '%s\n' "$@" >"$STUB_OUT/args"
env >"$STUB_OUT/env"
echo '[]'
SH
chmod +x "$stubs/zizmor"

# stub_run NAME [VAR=value ...]: scripts/zizmor.sh with the stub first on the PATH, in an otherwise empty
# environment, with the variables of the arguments and with those that must be removed.
stub_run() {
	stub_name=$1
	shift
	fresh
	rm -f "$scratch/args" "$scratch/env"
	status=0
	env -i PATH="$stubs:$PATH" STUB_OUT="$scratch" ZIZMOR_SELF_REPOSITORY_FINDINGS=0 \
		ZIZMOR_OFFLINE=true ZIZMOR_NO_ONLINE_AUDITS=true ZIZMOR_CONFIG=/nonexistent GH_HOST=example.invalid \
		ZIZMOR_GITHUB_TOKEN=z "$@" "$repo/scripts/zizmor.sh" "$dir" >"$scratch/out" 2>&1 || status=$?
	if [ "$status" -ne 0 ]; then
		echo "FAIL  $stub_name: the script failed with exit $status"
		stub_ok=0
		sed 's/^/        /' "$scratch/out"
		failures=$((failures + 1))
	fi
}
# check ASSERTION...: one assertion on what the stub recorded (has_arg ARG, lacks_arg ARG, has_env LINE,
# lacks_env NAME).
check() {
	if ! "$@"; then
		echo "FAIL  $stub_name: $*"
		failures=$((failures + 1))
		stub_ok=0
	fi
}
has_arg() { grep -Fxq -- "$1" "$scratch/args"; }
lacks_arg() { ! has_arg "$1"; }
has_env() { grep -Fxq -- "$1" "$scratch/env"; }
lacks_env() { ! grep -q "^$1=" "$scratch/env"; }
fixed_options() {
	for arg in --no-config --collect=all --strict-collection --no-progress --no-ignores --no-exit-codes .; do
		check has_arg "$arg"
	done
	check has_arg json
}
no_audit_variable() {
	for name in ZIZMOR_OFFLINE ZIZMOR_NO_ONLINE_AUDITS ZIZMOR_CONFIG GH_HOST ZIZMOR_GITHUB_TOKEN; do
		check lacks_env "$name"
	done
}

stub_ok=1
stub_run "offline without a token"
fixed_options
no_audit_variable
check has_arg --offline
check lacks_env GH_TOKEN
check lacks_env GITHUB_TOKEN
[ "$stub_ok" = 1 ] && echo "ok    no token: offline, the fixed options, no variable that changes the audit"

stub_ok=1
stub_run "offline with empty token variables" GH_TOKEN= GITHUB_TOKEN=
fixed_options
no_audit_variable
check has_arg --offline
check lacks_env GH_TOKEN
check lacks_env GITHUB_TOKEN
[ "$stub_ok" = 1 ] && echo "ok    empty token variables count as no token"

stub_ok=1
stub_run "online with GH_TOKEN" GH_TOKEN=first
fixed_options
no_audit_variable
check lacks_arg --offline
check has_env GH_TOKEN=first
check lacks_env GITHUB_TOKEN
[ "$stub_ok" = 1 ] && echo "ok    GH_TOKEN: online, the fixed options, no variable that changes the audit"

stub_ok=1
stub_run "online with GITHUB_TOKEN only" GITHUB_TOKEN=second
fixed_options
no_audit_variable
check lacks_arg --offline
check has_env GH_TOKEN=second
check lacks_env GITHUB_TOKEN
[ "$stub_ok" = 1 ] && echo "ok    GITHUB_TOKEN alone is passed on as GH_TOKEN"

if [ "$failures" -ne 0 ]; then
	echo "check-zizmor-options: $failures probe(s) failed" >&2
	exit 1
fi
echo "check-zizmor-options: every input the tree can carry is ignored or refused"
