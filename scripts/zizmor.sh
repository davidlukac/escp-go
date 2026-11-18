#!/bin/sh
# Runs zizmor over the .github/ directory of a repository root (default: the current directory) with
# fixed options, so that the result does not depend on files in the checked-out tree: no
# configuration file is read (--no-config), every workflow is collected, whatever an ignore file
# says (--collect=all), and a file that cannot be loaded fails the run instead of being skipped with a
# warning (--strict-collection). It is the one place the command line is built: `make zizmor` calls it, and so
# do the probes of scripts/check-zizmor-options.sh.
#
# Inline suppressions are an allow-list: the workflows may carry exactly
# ZIZMOR_SELF_REPOSITORY_IGNORES comments of the form `# zizmor: ignore[self-repository]` on a
# `uses: ./.github/actions/setup` line (the Makefile holds the number), and no other zizmor ignore
# comment anywhere in .github/, so a new suppression is a deliberate edit of that number in the same
# pull request. A suppression hides a finding from the check and from a reviewer reading only the
# diff of the findings.
#
# It audits online (the audits that look up an action's history) when a GitHub token is in GH_TOKEN
# or GITHUB_TOKEN, offline otherwise. An empty variable counts as no token.
set -eu

root=${1:-.}
allowed=${ZIZMOR_SELF_REPOSITORY_IGNORES:?set it to the number of self-repository ignores the workflows may carry}
cd "$root"

# Every line that mentions a zizmor ignore, in any spelling zizmor would read, and the ones that are
# allowed.
mention='zizmor:[[:space:]]*ignore'
allowed_form='^[^:]*:[0-9]*:[[:space:]]*(- )?uses: \./\.github/actions/setup # zizmor: ignore\[self-repository\]( |$)'
found=$(grep -rniE "$mention" .github || true)
if [ -n "$found" ]; then
	total=$(printf '%s\n' "$found" | wc -l | tr -d ' ')
	known=$(printf '%s\n' "$found" | grep -cE "$allowed_form" || true)
else
	total=0
	known=0
fi
if [ "$total" != "$known" ] || [ "$known" != "$allowed" ]; then
	echo "zizmor ignore comments: found $total, of the allowed form $known, allowed $allowed (ZIZMOR_SELF_REPOSITORY_IGNORES in the Makefile)." >&2
	printf '%s\n' "$found" | grep -vE "$allowed_form" >&2 || true
	exit 1
fi

set -- --no-config --collect=all --strict-collection --no-progress
token=${GH_TOKEN:-${GITHUB_TOKEN:-}}
if [ -n "$token" ]; then
	GH_TOKEN=$token exec zizmor "$@" .github/
fi
echo "no GH_TOKEN: offline audits only"
# zizmor reads GH_TOKEN itself and rejects an empty value, so the variables are removed.
exec env -u GH_TOKEN -u GITHUB_TOKEN zizmor --offline "$@" .github/
