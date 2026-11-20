# Dead-code fixtures

A module of its own that holds one function only a test calls (`testonly.go`), one that production code
calls and one exported function nothing calls (`used.go`). `make lintfixtures` (part of `make lint`) runs
the dead-code configuration, `.golangci-deadcode.yml`, over a copy of it with the linters that configuration
enables (not a list given on the command line, which would hide a configuration that enables the wrong
linter), and compares what the linter reports with [expected.txt](expected.txt): `onlyATest` must be reported
and nothing else.

A configuration weakened so that `make deadcode` can no longer find code only a test uses (`run.tests: true`,
another linter instead of `unused`, no linter at all) leaves the finding missing and fails the target
(DC-C05, issue #260). The ordinary `unused` run of the repository would not report `onlyATest`: it has the
tests on.

The fixtures only have to compile: they are never run.
