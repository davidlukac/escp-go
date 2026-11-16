# Lint fixtures

A module of its own (so `go test ./...`, `go vet ./...` and the lint of the library never see it) that holds
the violations the layering and `forbidigo` rules of `.golangci.yml` exist to catch, and, in the places those
rules leave out, the code that must stay clean. `make lintfixtures` (part of `make lint`) runs the repository's
own configuration over a copy of it and compares what the linter reports with [expected.txt](expected.txt):
every expected finding must appear and nothing else. A rule whose pattern stops matching, an exclusion that
swallows a fixture, or a rule that was relaxed fails the target (issue #252; the rules are the ones of
[DC-C06](../../docs/defect-classes.md#dc-c06), [DC-L09](../../docs/defect-classes.md#dc-l09) and the layering of
[architecture.md](../../docs/architecture.md)).

- The module path is the library's own, on purpose: the `depguard` rules name packages of that path.
- The files are named and placed like the real ones where a rule is path-based (`transport.go`,
  `discover.go`, `model/`, `safety/`, `escptest/`, `examples/`, `internal/coverpolicy/`, `_test.go`).
  The checker copies the module next to a copy of the configuration before it runs the linter, because the
  paths of the exclusions are relative to the configuration.
- A file that must stay clean says so in its doc comment; a file with a violation says which rule it breaks.
- A new `depguard` rule or `forbidigo` pattern, and a new exclusion, comes with its fixture and its line in
  `expected.txt` in the same pull request.
- The fixtures only have to compile: they are never run.
