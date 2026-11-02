#!/bin/sh
# Renders the package documentation the way pkg.go.dev does, from the working tree, and saves
# each page as HTML and as plain text under OUT (default build/pkgdoc, gitignored).
#
#   scripts/pkgdoc.sh [OUT]          render and save
#   scripts/pkgdoc.sh --serve        serve on http://localhost:8089 until interrupted
#
# A thin wrapper: the work is done by internal/pkgdoc (Go, tested), which runs the pkgsite version
# pinned in devtools/go.mod. PKGDOC_PORT changes the port.
set -eu
cd "$(git rev-parse --show-toplevel)"
exec go run ./internal/pkgdoc "$@"
