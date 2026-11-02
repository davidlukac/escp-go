#!/bin/sh
# Usage: scripts/release.sh X.Y.Z "Short changelog line"     (on a branch)
#        scripts/release.sh --tag X.Y.Z                      (on main, after the squash merge)
#
# A thin wrapper: the work is done by internal/release (Go, tested), see its package comment and
# RELEASING.md. Set RELEASE_COMMIT_TRAILER to a whole trailer line (for example
# "Co-Authored-By: Name <name@example.org>") to have it added to the release commit.
set -eu
cd "$(git rev-parse --show-toplevel)"
exec go run ./internal/release "$@"
