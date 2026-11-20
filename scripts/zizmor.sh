#!/bin/sh
# Runs zizmor over a whole repository (the root given, default: the current directory: the workflows,
# every composite action wherever it is, the Dependabot configuration) with fixed options, so that
# the result does not depend on files in the checked-out tree: no configuration file is read
# (--no-config), every workflow is collected, whatever an ignore file says (--collect=all), and a
# file that cannot be loaded fails the run instead of being skipped with a warning
# (--strict-collection). It is the one place the command line is built: `make zizmor` calls it, and
# so do the probes of internal/zizmorprobe.
#
# The findings are judged against an exact expectation, not against zizmor's own exit status: zizmor is
# run with --no-ignores, so an ignore comment in a workflow changes nothing, and the run passes only
# when its findings are exactly ZIZMOR_SELF_REPOSITORY_FINDINGS findings of the `self-repository` rule
# and nothing else (the Makefile holds the number and says why). A new suppression, a removed use, or
# a finding of any other rule is therefore a deliberate edit of that number in the same pull request,
# or a failure. The comments on those uses stay for the reader: they say why the form is used.
# A symbolic link under .github/ is refused: it would make the files audited depend on where it points.
#
# It audits online (the audits that look up an action's history) when a GitHub token is in GH_TOKEN
# or GITHUB_TOKEN, offline otherwise. An empty variable counts as no token. It needs the go tool, to run
# the program that reads zizmor's JSON (internal/zizmorverdict): that is the one place the answer is read,
# the number is validated there too (a plain non-negative integer), and it fails closed on anything
# it does not understand.
set -eu

root=${1:-.}
expected=${ZIZMOR_SELF_REPOSITORY_FINDINGS:?set it to the number of self-repository findings the workflows may carry}
repo=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

links=$(find .github -type l)
if [ -n "$links" ]; then
	echo "symbolic links under .github/ are not allowed:" >&2
	echo "$links" >&2
	exit 1
fi

token=${GH_TOKEN:-${GITHUB_TOKEN:-}}
if [ -z "$token" ]; then
	echo "no GH_TOKEN: offline audits only"
fi

# Every environment variable that can change what zizmor 1.32.0 audits or how it reaches GitHub is
# removed, so that a variable set by a workflow step or by the machine cannot narrow the audit. From
# `zizmor --help` and the binary:
#   ZIZMOR_OFFLINE, ZIZMOR_NO_ONLINE_AUDITS  would turn the online audits off silently: removed.
#   ZIZMOR_CONFIG                            would name a configuration file (it conflicts with
#                                            --no-config, which is passed): removed.
#   GH_HOST                                  would point the API calls, and the token, at another
#                                            host: removed.
#   GH_TOKEN, GITHUB_TOKEN, ZIZMOR_GITHUB_TOKEN  the token: removed, then GH_TOKEN is set from the
#                                            variables above (online) or the run is --offline.
#   ZIZMOR_RENDER_LINKS, ZIZMOR_SHOW_AUDIT_URLS, RUST_LOG, NO_COLOR and the other color variables
#                                            shape the plain output and the log only; the verdict
#                                            reads --format json: they cannot matter.
#   HOME, XDG_CACHE_HOME, XDG_CONFIG_HOME, GIT_CONFIG_*  where the HTTP cache and the git
#                                            configuration are: the ignore files are not honored
#                                            (--collect=all) and a stale cache entry is the same
#                                            answer GitHub gave before: they cannot matter.
# Options that are not passed keep their default: --persona, --min-severity and --min-confidence
# have no environment variable and no configuration file to change them (--no-config); --fix, --lsp,
# --completions, --thanks and --cache-dir do not change what is audited.
clean_env() {
	env -u ZIZMOR_OFFLINE -u ZIZMOR_NO_ONLINE_AUDITS -u ZIZMOR_CONFIG -u GH_HOST \
		-u ZIZMOR_GITHUB_TOKEN -u GH_TOKEN -u GITHUB_TOKEN "$@"
}

audit() {
	if [ -n "$token" ]; then
		clean_env GH_TOKEN="$token" zizmor --no-config --collect=all --strict-collection --no-progress "$@" .
	else
		clean_env zizmor --offline --no-config --collect=all --strict-collection --no-progress "$@" .
	fi
}

findings=$(mktemp)
trap 'rm -f "$findings"' EXIT
# --no-exit-codes: a finding is not a failure here, the verdict is below; a tool failure still is.
# The format is named (json-v1, which is also what `json` means in 1.32.0), so that a change of the
# default is not read as a different answer.
audit --no-ignores --no-exit-codes --format json-v1 >"$findings"

# The verdict is a Go program of this repository (internal/zizmorverdict, tested and fuzzed): the number
# expected on the command line, zizmor's answer on the standard input. It is built and run from the
# directory of this script's repository, never from the audited tree, so that no file of the tree (a
# go.mod, a go.work, a vendor directory) can change which program judges it.
go -C "$repo" run ./internal/zizmorverdict "$expected" <"$findings"
