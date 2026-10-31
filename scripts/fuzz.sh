#!/bin/sh
# Runs every fuzz target of the module for the given number of seconds each (default 30).
# The seed corpora (testdata/fuzz) already run as ordinary tests in `make test`; this explores
# beyond them. A failure leaves its input under the package's testdata/fuzz: commit it with the fix.
set -eu
cd "$(git rev-parse --show-toplevel)"
secs="${1:-30}"
status=0
for pkg in $(go list ./...); do
	for target in $(go test -list '^Fuzz' "$pkg" | grep '^Fuzz' || true); do
		echo "== $pkg $target (${secs}s)"
		go test "$pkg" -run '^$' -fuzz "^${target}\$" -fuzztime "${secs}s" || status=1
	done
done
exit $status
