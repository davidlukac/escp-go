.DEFAULT_GOAL := test

# Development tools (the linter, the vulnerability scanner, the API-compatibility checker) are pinned in
# devtools/go.mod, a module of their own so that their dependencies stay out of this module's go.mod;
# Dependabot bumps them there. `go tool` builds the pinned version on first use (cached afterwards).
DEVTOOL := go tool -modfile=devtools/go.mod

.PHONY: test lint spell actionlint modcheck vuln cover cover-html apidiff fuzz mutation mutation-diff pkgdoc pkgdoc-serve compat-table settings-docs soak hooks pre-commit

# Unit tests against the fake switch. No hardware is touched.
test:
	go test -race -count=1 ./...
	go -C devtools test ./...

# The linter checks the files the build includes, so a file for another operating system (the Linux
# sharing test, the Windows socket option) is only seen by a run on that system. CI runs on Linux,
# macOS and Windows; the hook runs on one, so the others are linted here by setting GOOS (the tool
# binary is built for this machine first: `go tool -n` prints its path).
LINT_GOOS := linux windows

# American English in every tracked text file, not only the Go comments the misspell linter reads: the
# Markdown, YAML, shell and the Makefile too. The same word list as the linter (misspell, locale US, pinned in
# devtools). History that quotes the past (the changelog, the code of conduct) is left as it was.
spell:
	@git ls-files -z -- '*.go' '*.md' '*.yml' '*.yaml' '*.json' '*.sh' Makefile ':!:CHANGELOG.md' ':!:CODE_OF_CONDUCT.md' ':!:text_test.go' ':!:testdata' \
		| xargs -0 $(DEVTOOL) misspell -locale US -error

# What `lint` runs, in order: the spelling check; the project's own checks, which golangci-lint has no
# rule for (devtools/subtests: a table of cases runs each case with t.Run; devtools/analyzers: `readwrite`,
# a read followed by a write outside the modify guard, and `lockwait`, a blocking operation under a lock
# without a `// lockwait: <reason>`), after the tests of the checks themselves; then golangci-lint, for
# every build tag and operating system.
lint: spell
	go -C devtools build -o $(CURDIR)/build/subtests ./subtests
	go -C devtools build -o $(CURDIR)/build/analyzers ./analyzers
	go -C devtools test -count=1 ./...
	build/subtests ./...
	build/analyzers ./...
	$(DEVTOOL) golangci-lint run ./...
	$(DEVTOOL) golangci-lint run --build-tags live ./...
	@bin="$$($(DEVTOOL) -n golangci-lint)"; for os in $(LINT_GOOS); do echo "golangci-lint run (GOOS=$$os)"; GOOS=$$os "$$bin" run ./... || exit 1; done

# The workflow files, with the pinned actionlint (it runs shellcheck on the scripts when that is installed);
# the pre-commit hook runs it for a staged workflow, CI for every pull request.
actionlint:
	$(DEVTOOL) actionlint

# The module files: tidy would change nothing (this module and devtools), and every downloaded
# dependency matches go.sum.
modcheck:
	go mod tidy -diff
	go -C devtools mod tidy -diff
	go mod verify
	go -C devtools mod verify

# Known vulnerabilities in the code and its dependencies.
vuln:
	$(DEVTOOL) govulncheck ./...

# `make cover` runs the whole suite with the race detector, writes cover.out and enforces the policy:
# every statement covered except functions whose doc comment says `// coverage: ignore - <reason>`
# (see CONTRIBUTING.md). The checker is Go (internal/coverpolicy) and is tested, and measured, by the same run.
cover:
	go test ./... -race -count=1 -coverpkg=./... -coverprofile=cover.out
	go run ./internal/coverpolicy check cover.out .

cover-html: cover
	go tool cover -html=cover.out -o coverage.html

# What changed in the exported API since the latest release (or since VERSION=vX.Y.Z). Informational
# while the module is 0.x: breaking changes are allowed, but the CHANGELOG must name them.
apidiff:
	$(DEVTOOL) gorelease $(if $(VERSION),-base=$(VERSION))

# Every fuzz target for SECONDS (default 30) each; the seed corpora run in `make test` anyway.
fuzz:
	scripts/fuzz.sh $(or $(SECONDS),30)

# Mutation testing (gremlins, pinned in devtools/go.mod): every covered conditional, boundary and arithmetic
# operator of the code is flipped one at a time and the tests of its package must fail. A mutant that
# survives is a behavior no test pins. `make mutation` runs them all (about 15 minutes with 8 workers;
# EFFICACY=90 fails below that share of caught mutants, WORKERS=n sets the parallelism, default 4);
# `make mutation-diff` only the lines changed since BASE (default origin/main), which is what a pull
# request runs. The report is build/mutation.json. Left out: the fake switch (escptest, judged by the
# tests of the code that uses it) and the programs (the coverage tool, the report tool, the release, fuzz and documentation tools, the example, and the
# devtools module, whose analyzer has fixtures of its own and no test in this module that gremlins could attribute to it):
# gremlins reports every mutant in a `package main` as surviving whatever the tests do (a mutant applied
# by hand to the report tool is caught by its tests), so their numbers would only be noise.
# GOFLAGS=-count=1: gremlins sizes the timeout of a mutant from the time of its first test run, and a cached
# result takes no time, so without it every mutant of a package whose tests ran before "times out".
MUTATE := GOFLAGS=-count=1 $(DEVTOOL) gremlins unleash . --workers $(or $(WORKERS),4) --coverpkg ./... --output build/mutation.json \
	-E '^escptest/' -E '^internal/coverpolicy/' -E '^internal/mutationreport/' -E '^internal/release/' -E '^internal/fuzzrun/' -E '^internal/pkgdoc/' -E '^devtools/' -E '^examples/' \
	$(if $(EFFICACY),--threshold-efficacy $(EFFICACY))

# gremlins does not create the directory of its report: without it the run ends with "impossible to
# write file" and the job summary says there was no report.
mutation:
	@mkdir -p build
	$(MUTATE)

mutation-diff:
	@mkdir -p build
	$(MUTATE) --diff $(or $(BASE),origin/main)

# The package documentation as pkg.go.dev renders it, from the working tree: HTML and plain text
# per package under build/pkgdoc (gitignored). `make pkgdoc-serve` browses it on localhost:8089.
pkgdoc:
	scripts/pkgdoc.sh

pkgdoc-serve:
	scripts/pkgdoc.sh --serve

# Rewrites the compatibility table in README.md from safety/compatibility.json (the firmware
# allow-list reads the same file); a test fails when the README is stale.
compat-table:
	go test ./safety -run TestReadmeCompatibilityTable -update-readme

# Rewrites the settings table of the README and the class lists of docs/writing-safely.md from model.Settings
# (model/settings.go); a test fails when they are stale.
settings-docs:
	go test . -run TestTheGeneratedTablesAreCurrent -update-docs

# What the weekly workflow runs: the suite shuffled and repeated with the race detector, and the
# leak test at 200 cycles (it takes a few minutes).
soak:
	ESCP_SOAK=200 go test -race -shuffle=on -count=3 ./...

# Enable the repository's git hooks (pre-commit runs `make pre-commit`).
hooks:
	git config core.hooksPath .githooks
	@echo "git hooks enabled: .githooks/pre-commit"

# What the pre-commit hook runs: format, vet, lint, then the whole suite with the race detector and
# the coverage policy. Same checks CI enforces.
pre-commit:
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed on:"; gofmt -l .; exit 1; }
	go vet ./...
	go vet -tags live ./...
	$(MAKE) lint
	$(MAKE) cover
