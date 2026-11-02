#!/bin/sh
# Runs every fuzz target of the module for the given number of seconds each (default 30).
# The seed corpora (testdata/fuzz) already run as ordinary tests in `make test`; this explores
# beyond them. A failure leaves its input under the package's testdata/fuzz: commit it with the fix.
#
# A thin wrapper: the work is done by internal/fuzzrun (Go, tested).
set -eu
cd "$(git rev-parse --show-toplevel)"
exec go run ./internal/fuzzrun "$@"
