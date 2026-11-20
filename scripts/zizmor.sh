#!/bin/sh
# Audits a repository with zizmor (the root given, default: the current directory) and judges what it finds,
# with fixed options and a cleaned environment so that nothing in the audited tree changes the result. A thin
# wrapper: the work is done by internal/zizmorrun (Go, tested; its package audit documents the options, the
# variables that are cleared and why, the refusal of symbolic links under .github/ and the exact expectation).
# `make zizmor` calls it, and so do the probes of internal/zizmorprobe.
#
# It needs the go tool. The program is built and run from the repository this script lives in, never from the
# audited tree, so that no file of the tree (a go.mod, a go.work, a vendor directory) can change which program
# runs; the root is made absolute first because `go -C` changes the directory.
set -eu

repo=$(cd "$(dirname "$0")/.." && pwd)
root=$(cd "${1:-.}" && pwd)
expected=${ZIZMOR_SELF_REPOSITORY_FINDINGS:?set it to the number of self-repository findings the workflows may carry}
exec go -C "$repo" run ./internal/zizmorrun "$root" "$expected"
