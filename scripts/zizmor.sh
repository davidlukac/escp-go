#!/bin/sh
# Runs zizmor over a whole repository (the root given, default: the current directory: the workflows,
# every composite action wherever it is, the Dependabot configuration) with fixed options, so that the result does not depend on files in the checked-out tree: no
# configuration file is read (--no-config), every workflow is collected, whatever an ignore file
# says (--collect=all), and a file that cannot be loaded fails the run instead of being skipped with a
# warning (--strict-collection). It is the one place the command line is built: `make zizmor` calls
# it, and so do the probes of scripts/check-zizmor-options.sh.
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
# or GITHUB_TOKEN, offline otherwise. An empty variable counts as no token. It needs python3, which
# zizmor's own installation already needs (it is a PyPI package), to read zizmor's JSON.
set -eu

root=${1:-.}
expected=${ZIZMOR_SELF_REPOSITORY_FINDINGS:?set it to the number of self-repository findings the workflows may carry}
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
audit --no-ignores --no-exit-codes --format json >"$findings"

verdict=$(cat <<'PY'
import json
import sys

expected = int(sys.argv[1])
with open(sys.argv[2], encoding="utf-8") as handle:
    findings = json.load(handle)

def place(finding):
    for location in finding["locations"]:
        path = location["symbolic"]["key"]["Local"]["verbatim_path"]
        row = location["concrete"]["location"]["start_point"]["row"] + 1
        return "%s:%d" % (path, row)
    return "?"

allowed = [f for f in findings if f["ident"] == "self-repository"]
others = [f for f in findings if f["ident"] != "self-repository"]
for finding in others:
    print("%s: %s (%s)" % (place(finding), finding["desc"], finding["ident"]), file=sys.stderr)
if others or len(allowed) != expected:
    print("zizmor findings: %d of the allowed rule (self-repository), %d of other rules; allowed exactly %d of the first and none of the second (ZIZMOR_SELF_REPOSITORY_FINDINGS in the Makefile)" % (len(allowed), len(others), expected), file=sys.stderr)
    sys.exit(1)
print("zizmor: no findings but the %d allowed ones" % expected)
PY
)
python3 -I -c "$verdict" "$expected" "$findings"
