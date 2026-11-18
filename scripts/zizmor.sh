#!/bin/sh
# Runs zizmor over the .github/ directory of a repository root (default: the current directory) with
# fixed options, so that the result does not depend on files in the checked-out tree: no
# configuration file is read (--no-config) and every workflow is collected, whatever an ignore file
# says (--collect=all). It is the one place the command line is built: `make zizmor` calls it, and so
# do the probes of scripts/check-zizmor-options.sh.
#
# It audits online (the audits that look up an action's history) when a GitHub token is in GH_TOKEN
# or GITHUB_TOKEN, offline otherwise. An empty variable counts as no token.
set -eu

root=${1:-.}
cd "$root"

set -- --no-config --collect=all --no-progress
token=${GH_TOKEN:-${GITHUB_TOKEN:-}}
if [ -n "$token" ]; then
	GH_TOKEN=$token exec zizmor "$@" .github/
fi
echo "no GH_TOKEN: offline audits only"
# zizmor reads GH_TOKEN itself and rejects an empty value, so the variables are removed.
exec env -u GH_TOKEN -u GITHUB_TOKEN zizmor --offline "$@" .github/
