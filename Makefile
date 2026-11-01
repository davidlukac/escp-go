.DEFAULT_GOAL := test

# Development tools (the linter, the vulnerability scanner, the API-compatibility checker) are pinned in
# devtools/go.mod, a module of their own so that their dependencies stay out of this module's go.mod;
# Dependabot bumps them there. `go tool` builds the pinned version on first use (cached afterwards).
DEVTOOL := go tool -modfile=devtools/go.mod

.PHONY: test lint vuln cover cover-html apidiff fuzz mutation mutation-diff pkgdoc pkgdoc-serve compat-table hooks pre-commit

# Unit tests against the fake switch. No hardware is touched.
test:
	go test -race -count=1 ./...

lint:
	$(DEVTOOL) golangci-lint run ./...
	$(DEVTOOL) golangci-lint run --build-tags live ./...

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
# survives is a behaviour no test pins. `make mutation` runs them all (about 15 minutes with 8 workers;
# EFFICACY=90 fails below that share of caught mutants, WORKERS=n sets the parallelism);
# `make mutation-diff` only the lines changed since BASE (default origin/main), which is what a pull
# request runs. The report is build/mutation.json. Left out: the fake switch (escptest, judged by the
# tests of the code that uses it), the coverage tool and the example program.
MUTATE := $(DEVTOOL) gremlins unleash . --workers $(or $(WORKERS),4) --coverpkg ./... --output build/mutation.json \
	-E '^escptest/' -E '^internal/coverpolicy/' -E '^examples/' $(if $(EFFICACY),--threshold-efficacy $(EFFICACY))

mutation:
	$(MUTATE)

mutation-diff:
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
