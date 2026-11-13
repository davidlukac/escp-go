.DEFAULT_GOAL := test

# Development tools (the linter, the vulnerability scanner, the API-compatibility checker) are pinned in
# devtools/go.mod, a module of their own so that their dependencies stay out of this module's go.mod;
# Dependabot bumps them there. `go tool` builds the pinned version on first use (cached afterwards).
DEVTOOL := go tool -modfile=devtools/go.mod

.PHONY: test lint spell actionlint modcheck vuln cover cover-html apidiff fuzz mutation mutation-diff mutation-extended pkgdoc pkgdoc-serve compat-table settings-docs soak soak-idle soak-single soak-loaded hooks pre-commit

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
# a read followed by a write outside the modify guard, `lockwait`, a blocking operation under a lock
# without a `// lockwait: <reason>`, `validatecall`, an exported function that takes a value with a
# `Validate` method and does not call it, and `nosleep`, a sleep or a guess-wait on the clock without a
# `// nosleep: <reason>`, run a second time with the `live` tag), after the tests of the checks themselves; then golangci-lint, for
# every build tag and operating system.
lint: spell
	go -C devtools build -o $(CURDIR)/build/subtests ./subtests
	go -C devtools build -o $(CURDIR)/build/analyzers ./analyzers
	go -C devtools test -count=1 ./...
	build/subtests ./...
	build/analyzers ./...
	GOFLAGS=-tags=live build/analyzers -nosleep ./...
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

# Two checks of the project's own, run by `make lint` (they are prerequisites of it, so a change to the
# lint recipe does not touch them):
#   catalogcheck  every name the defect catalog gives to a check resolves: a renamed test or a removed
#                 target fails here (devtools/catalogcheck; it exits 1 with one line per problem);
#   guarantee     the exported identifiers whose doc comment states a guarantee and that no test names
#                 with a `// guarantee: <Name>` marker (devtools/guarantee, run with -strict: it lists
#                 them and fails; without the flag it only reports).
.PHONY: catalogcheck guarantee
lint: catalogcheck guarantee

catalogcheck:
	go -C devtools build -o $(CURDIR)/build/catalogcheck ./catalogcheck
	build/catalogcheck -root .

guarantee:
	go -C devtools build -o $(CURDIR)/build/guarantee ./guarantee
	build/guarantee -strict -root .

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
# PKG=<directory> (model, safety, or . for the root package) limits a run to the files of that directory:
# gremlins takes only the module root as its path, so every other directory with Go files is excluded.
PKG_DIRS := $(patsubst ./%,%,$(sort $(shell find . -name '*.go' -not -path './build/*' -not -path './.git/*' -not -path './devtools/*' -not -path '*/testdata/*' -exec dirname {} \;)))
PKG_EXCLUDES := $(if $(PKG),$(foreach d,$(filter-out $(PKG),$(PKG_DIRS)),-E '^$(if $(filter .,$(d)),,$(d)/)[^/]+$$'))
GREMLINS := GOFLAGS=-count=1 $(DEVTOOL) gremlins unleash . --workers $(or $(WORKERS),4) --coverpkg ./... $(PKG_EXCLUDES) \
	-E '^escptest/' -E '^internal/coverpolicy/' -E '^internal/mutationreport/' -E '^internal/release/' -E '^internal/seams/' -E '^internal/fuzzrun/' -E '^internal/soak/' -E '^internal/pkgdoc/' -E '^devtools/' -E '^examples/'
MUTATE := $(GREMLINS) --output build/mutation.json $(if $(EFFICACY),--threshold-efficacy $(EFFICACY))

# The extended operators: the ones gremlins ships and leaves off (invert-logical swaps && and ||,
# invert-assignments turns += into -=, invert-bitwise swaps & | ^ << >>, invert-bwassign the same for
# the assignment forms, invert-loopctrl swaps break and continue, remove-self-assignments drops x += y
# to x = y), with the five default operators of `make mutation` switched off, so that the two runs
# are two disjoint sets and their numbers add up. `make mutation-extended` runs them over the module
# (a quarter of an hour or more with 4 workers) into build/mutation-extended.json; PKG=model limits a
# run to one directory (the quick way to look at one area; PKG works for the other mutation targets too). Informational: there is no efficacy threshold on
# purpose, the weekly workflow runs it with continue-on-error and prints the survivors in its summary.
EXTENDED_ON := --invert-assignments --invert-bitwise --invert-bwassign --invert-logical --invert-loopctrl --remove-self-assignments
DEFAULTS_OFF := --arithmetic-base=false --conditionals-boundary=false --conditionals-negation=false --increment-decrement=false --invert-negatives=false
MUTATE_EXTENDED := $(GREMLINS) --output build/mutation-extended.json $(DEFAULTS_OFF) $(EXTENDED_ON)

# gremlins does not create the directory of its report: without it the run ends with "impossible to
# write file" and the job summary says there was no report.
mutation:
	@mkdir -p build
	$(MUTATE)

mutation-diff:
	@mkdir -p build
	$(MUTATE) --diff $(or $(BASE),origin/main)

mutation-extended:
	@mkdir -p build
	$(MUTATE_EXTENDED)

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

# What the weekly workflow runs, from one source (internal/soak, which the workflow calls as well): the
# suite shuffled and repeated with the race detector, three ways, and the leak test at 200 cycles
# (several minutes per way). `soak` runs all three; each has a target of its own:
#   soak-idle    nothing else running (the first soak, kept as it was);
#   soak-single  GOMAXPROCS=1, which interleaves goroutines in orders a multi-core run seldom produces;
#   soak-loaded  busy goroutines on every CPU beside the tests, which delay a goroutine for longer than
#                a short timeout (the stale-datagram defect of the drain, #206 and #207, showed only so).
# SOAK_FLAGS and SOAK_PACKAGES narrow a run, for example
# `make soak-loaded SOAK_FLAGS='-count 200 -run TestAStale' SOAK_PACKAGES=.` (see `go run ./internal/soak -h`).
SOAK := go run ./internal/soak $(SOAK_FLAGS)
SOAK_TAIL = $(if $(SOAK_PACKAGES),-- $(SOAK_PACKAGES))

soak:
	$(SOAK) $(SOAK_TAIL)

soak-idle:
	$(SOAK) idle $(SOAK_TAIL)

soak-single:
	$(SOAK) single $(SOAK_TAIL)

soak-loaded:
	$(SOAK) loaded $(SOAK_TAIL)

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
