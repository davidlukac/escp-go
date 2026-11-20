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
# `// nosleep: <reason>`, run a second time with the `live` tag, and `hostport`, a test client aimed at the fake
# that is left the real reply port without a `// hostport: <reason>`), after the tests of the checks themselves; then golangci-lint, for
# every build tag and operating system. The same configuration and the `subtests` check also run over
# devtools/recipecheck, which is Go like the rest (devtools is a module of its own, so `./...` above
# does not reach it); the other devtools packages have findings of their own and are not linted yet.
lint: spell
	go -C devtools build -o $(CURDIR)/build/subtests ./subtests
	go -C devtools build -o $(CURDIR)/build/analyzers ./analyzers
	go -C devtools test -count=1 ./...
	build/subtests ./...
	go -C devtools tool golangci-lint run --config ../.golangci.yml ./recipecheck/...
	go -C devtools tool golangci-lint run --config ../.golangci.yml --build-tags mutants ./recipecheck/...
	cd devtools && ../build/subtests ./recipecheck/...
	build/analyzers ./...
	GOFLAGS=-tags=live build/analyzers -nosleep ./...
	$(DEVTOOL) golangci-lint run ./...
	$(DEVTOOL) golangci-lint run --build-tags live ./...
	@bin="$$($(DEVTOOL) -n golangci-lint)"; for os in $(LINT_GOOS); do echo "golangci-lint run (GOOS=$$os)"; GOOS=$$os "$$bin" run ./... || exit 1; done

# The workflow files, with the pinned actionlint (it runs shellcheck on the scripts when that is installed);
# the pre-commit hook runs it for a staged workflow, CI for every pull request.
actionlint:
	$(DEVTOOL) actionlint

# The security of the workflow files, with zizmor at its default severity and persona (what a workflow
# may do and how untrusted input reaches it; CONTRIBUTING.md). It is a Python program, not a Go tool, so
# it is not in devtools/go.mod: the version is pinned, with hashes, in devtools/zizmor/requirements.txt, which
# CI installs and Dependabot bumps. internal/zizmorrun (a Go program, started by scripts/zizmor.sh) builds the command line, with fixed options so that
# the result does not depend on files in the checked-out tree: it runs online (the audits that look up
# an action's history) when a GitHub token is in GH_TOKEN or GITHUB_TOKEN, offline otherwise. Not part
# of the pre-commit hook.
#
# The findings are an exact allow-list: zizmor is run with --no-ignores (an ignore comment changes
# nothing) and the run passes only when it reports this many `self-repository` findings (one per use of
# the local setup action, which the workflows write as `./.github/actions/setup`) and nothing else. The
# rule is counted, not located: a use of another local action that the rule also reports counts the same,
# and is audited itself because the whole repository is the input. A new finding, a new use or a
# removed one is a deliberate edit of this number in the same pull request. The answer (--format json-v1)
# is read by the same program (package internal/zizmorverdict/verdict), which fails closed on anything it does not understand. The number goes away, with the comments on those uses, when actionlint accepts the `$/` form of a local
# action (rhysd/actionlint#711).
ZIZMOR_SELF_REPOSITORY_FINDINGS := 13

.PHONY: zizmor
zizmor:
	@command -v zizmor >/dev/null 2>&1 || { \
		echo "zizmor is not installed. The pinned version, with hashes:"; \
		echo "  python3 -m venv ~/.cache/escp-go-zizmor && ~/.cache/escp-go-zizmor/bin/pip install --require-hashes -r devtools/zizmor/requirements.txt"; \
		echo "then put ~/.cache/escp-go-zizmor/bin on PATH."; \
		exit 1; }
	@pin="$$(sed -n 's/^zizmor==\([0-9][0-9.]*\).*/\1/p' devtools/zizmor/requirements.txt)"; \
	have="$$(zizmor --version | awk '{print $$2}')"; \
	[ "$$have" = "$$pin" ] || echo "warning: zizmor $$have is installed, devtools/zizmor/requirements.txt pins $$pin"
	@ZIZMOR_SELF_REPOSITORY_FINDINGS=$(ZIZMOR_SELF_REPOSITORY_FINDINGS) scripts/zizmor.sh

# Records GitHub's own rendering of the descriptions in internal/mergereadiness/readiness/testdata/oracle
# (NN-name.md -> NN-name.html), the ground truth the tests of the "Merge readiness" check compare the
# parser with (it must report at least as many unticked boxes as the renderer shows). It needs `gh auth`
# and the network, posts nothing (the markdown endpoint only renders), and is not run in CI: run it when
# a description is added or GitHub's renderer changes, and commit the HTML that changed. The renderer puts a
# random hash in the ids of a footnote; it is cut, so that a re-recording is identical.
.PHONY: oracle-refresh
oracle-refresh:
	@for f in internal/mergereadiness/readiness/testdata/oracle/*.md; do \
		gh api -X POST /markdown -F text=@"$$f" -f mode=gfm > "$${f%.md}.html.tmp" || exit 1; \
		sed -E 's/-[0-9a-f]{32}"/"/g' "$${f%.md}.html.tmp" > "$${f%.md}.html"; rm -f "$${f%.md}.html.tmp"; \
	done

# The module files: tidy would change nothing (this module and devtools), and every downloaded
# dependency matches go.sum.
modcheck:
	go mod tidy -diff
	go -C devtools mod tidy -diff
	go mod verify
	go -C devtools mod verify

# Four checks of the project's own, run by `make lint` (they are prerequisites of it, so a change to the
# lint recipe does not touch them):
#   catalogcheck  every name the defect catalog gives to a check resolves: a renamed test or a removed
#                 target fails here (devtools/catalogcheck; it exits 1 with one line per problem);
#   guarantee     the exported identifiers whose doc comment states a guarantee and that no test names
#                 with a `// guarantee: <Name>` marker (devtools/guarantee, run with -strict: it lists
#                 them and fails; without the flag it only reports);
#   lintfixtures  the layering (depguard) and forbidigo rules of .golangci.yml still fire: the repository's
#                 own configuration is run over the fixture module devtools/lintfixtures, which holds one
#                 violation of each rule and, in the places a rule leaves out, code that must stay clean,
#                 and the findings are compared with devtools/lintfixtures/expected.txt, every one present
#                 and nothing else (devtools/lintcheck; about a second). A rule whose pattern stops
#                 matching, an exclusion that swallows a fixture or a rule that was relaxed fails here;
#   zizmorcheck   scripts/zizmor.sh, the command `make zizmor` runs (a few lines that start internal/zizmorrun), still reports a deliberate finding
#                 next to each input the audited tree can carry (a configuration file, an ignore file, an
#                 extra inline suppression, a file zizmor cannot load, a link, a stub zizmor that records the command line
#                 and the environment; internal/zizmorprobe, a Go program, offline). It needs the pinned
#                 zizmor: without it the check says so and passes, and the `zizmor` jobs of CI, which
#                 install it, set ZIZMOR_REQUIRED so that it cannot be skipped there.
.PHONY: catalogcheck guarantee lintfixtures mutationrecipe recipemutants zizmorcheck
lint: catalogcheck guarantee lintfixtures mutationrecipe zizmorcheck

# The mutation recipes (devtools/recipecheck): `make -n` of the three mutation targets with the
# variables the jobs use, every recipe line compared whole, and make run with a stand-in for gremlins to
# see the exit codes (a `-` prefix on a step is invisible to `make -n`): the old report is deleted before
# the run, with EFFICACY set (an explicitly empty one too) the threshold is validated first and the check
# runs after gremlins, the timeout coefficient is validated before it, no `--threshold-efficacy`, and so
# on; the working tree's build/mutation.json is put aside and back. A deleted `ifneq` block, a check
# made non-fatal or a coefficient dropped fails here.
mutationrecipe:
	go -C devtools run ./recipecheck -root $(CURDIR)

# The same check, held to the mutants of the Makefile it must catch (devtools/recipecheck/testdata/
# makefile_mutants.json): every mutant is applied to a copy of the Makefile and the check must fail on
# it, except the ones listed as equivalent with their reason. About a minute (the mutants run in
# parallel), so it is not part of `make lint`; CI runs it as a step of the lint job. The target passes the
# tag itself and fails unless the log has the PASS line of the test (`go test` exits 0 with "no tests to
# run" when a tag or a name is wrong), and `make mutationrecipe` pins both lines as written.
recipemutants:
	@mkdir -p build
	go -C devtools test -tags mutants -count=1 -v -timeout 15m -run '^TestMakefileMutants$$' ./recipecheck > build/recipemutants.log || { cat build/recipemutants.log; exit 1; }
	grep -e '^--- PASS: TestMakefileMutants ' build/recipemutants.log

catalogcheck:
	go -C devtools build -o $(CURDIR)/build/catalogcheck ./catalogcheck
	build/catalogcheck -root .

guarantee:
	go -C devtools build -o $(CURDIR)/build/guarantee ./guarantee
	build/guarantee -strict -root .

lintfixtures:
	go -C devtools build -o $(CURDIR)/build/lintcheck ./lintcheck
	build/lintcheck -root . -lint "$$($(DEVTOOL) -n golangci-lint)"
	build/lintcheck -root . -lint "$$($(DEVTOOL) -n golangci-lint)" -config .golangci-deadcode.yml -fixtures devtools/deadcodefixtures -linters ""

# Code that only a test uses. `make lint` runs `unused` with the tests on, so a function, a field or a dial
# option that a test calls counts as used and ships in the production build for nothing. This runs `unused`
# alone with the tests left out (.golangci-deadcode.yml: `run.tests: false`), with and without the `live`
# build tag, and fails on any finding. A prerequisite of `lint`. `make lintfixtures` holds the configuration to
# a fixture (devtools/deadcodefixtures) and `make mutationrecipe` pins the prerequisite and the recipe, so that
# a configuration weakened or a recipe switched off is noticed.
.PHONY: deadcode
lint: deadcode
deadcode:
	$(DEVTOOL) golangci-lint run --config .golangci-deadcode.yml ./...
	$(DEVTOOL) golangci-lint run --config .golangci-deadcode.yml --build-tags live ./...

zizmorcheck:
	@ZIZMOR_SELF_REPOSITORY_FINDINGS=$(ZIZMOR_SELF_REPOSITORY_FINDINGS) go run ./internal/zizmorprobe

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
# request runs. The report is build/mutation.json.
# The efficacy is (killed + timed out) / (killed + timed out + lived): a mutant that makes the tests hang
# is noticed by the test run, which is a kill (PIT and Stryker count it so), whereas gremlins leaves a
# timed-out mutant out of its own figure. So gremlins runs without --threshold-efficacy, and with
# EFFICACY set (`make mutation` and `make mutation-diff` both honor it) the target validates the number
# first (`internal/mutationreport threshold`, so that a typo fails at once and not after the run) and
# then runs `internal/mutationreport check` on the report, which applies that definition (the summary
# and the badge use the same function). The check passes AT the threshold (gremlins' own gate failed
# at equality). EFFICACY is tested by its origin, not its value: an explicitly empty EFFICACY (or one of
# spaces, which make 3.81 strips to nothing) is an error from `threshold`, never a silent skip; leave it
# unset for no check. The timed-out count is reported on a line of its own, with the mutants, and an
# unexplained rise is investigated: a slow test, a cached baseline (see GOFLAGS below), or a real
# infinite loop. A report with no mutant that was run and decided has no efficacy, and the check fails
# on it. gremlins exits 0 and writes no report when it has nothing to report (an empty scope, a canceled
# run), so the recipes delete the old report first: a stale one must never be judged in place of the
# run. `make mutationrecipe` (part of `make lint`) pins these recipes line by line.
# Left out: the fake switch (escptest, judged by the
# tests of the code that uses it) and the programs (the coverage tool, the report tool, the release, fuzz and documentation tools, the example, and the
# devtools module, whose analyzer has fixtures of its own and no test in this module that gremlins could attribute to it):
# gremlins reports every mutant in a `package main` as surviving whatever the tests do (a mutant applied
# by hand to the report tool is caught by its tests), so their numbers would only be noise.
# GOFLAGS=-count=1: gremlins sizes the timeout of a mutant from the time of its first test run, and a cached
# result takes no time, so without it every mutant of a package whose tests ran before "times out".
# PKG=<directory> (model, safety, or . for the root package) limits a run to the files of that directory:
# gremlins takes only the module root as its path, so every other directory with Go files is excluded.
# PKG_DIRS is lazy (`=`): the `find` runs only when PKG is set, not at every start of make (0.45 s, which the
# recipe self-check would pay about 55 times).
PKG_DIRS = $(patsubst ./%,%,$(sort $(shell find . -name '*.go' -not -path './build/*' -not -path './.git/*' -not -path './devtools/*' -not -path '*/testdata/*' -exec dirname {} \;)))
PKG_EXCLUDES := $(if $(PKG),$(foreach d,$(filter-out $(PKG),$(PKG_DIRS)),-E '^$(if $(filter .,$(d)),,$(d)/)[^/]+$$'))
# TIMEOUT_COEFFICIENT: gremlins gives every mutant the same time, the elapsed time of its one coverage run
# of the whole module (30 to 39 s in the CI logs; not of the tests of the mutant's package) x this
# coefficient (its default is 3), and marks a mutant that outlives it TIMED OUT, a status of time and not
# of what the tests said. A timeout counts as killed (see the efficacy above), so it has to mean "the
# tests hung" and not "the runner was slow": a loaded runner must not turn a surviving mutant into a
# counted kill. A hang therefore costs about 10 x 33 s, roughly 5.5 minutes (1.65 with 3). The weekly job
# (90-minute limit, about 19 minutes without hangs) tolerates about 50 hangs; the pull request job
# (timeout-minutes 30, about one minute) about 20 hangs in one diff. The last weekly run had one.
# It is validated before the run (`internal/mutationreport coefficient`: a plain integer from 1 to 9999). A
# negative one makes every mutant time out at once and the gate pass on nothing, 0 is gremlins' default 3,
# and a huge one overflows its budget (the elapsed time times the value, as nanoseconds, goes negative from
# about 3e8) with the same result; `010` is octal 8 to gremlins and 10 to a decimal parser, so only the
# plain form is taken.
# Override like WORKERS, on the command line or in the environment: `make mutation TIMEOUT_COEFFICIENT=20`.
TIMEOUT_COEFFICIENT ?= 10
GREMLINS := GOFLAGS=-count=1 $(DEVTOOL) gremlins unleash . --workers $(or $(WORKERS),4) --timeout-coefficient $(TIMEOUT_COEFFICIENT) --coverpkg ./... $(PKG_EXCLUDES) \
	-E '^escptest/' -E '^internal/coverpolicy/' -E '^internal/mutationreport/' -E '^internal/release/' -E '^internal/seams/' -E '^internal/fuzzrun/' -E '^internal/soak/' -E '^internal/pkgdoc/' -E '^devtools/' -E '^examples/'
MUTATE := $(GREMLINS) --output build/mutation.json

# The extended operators: the ones gremlins ships and leaves off (invert-logical swaps && and ||,
# invert-assignments turns += into -=, invert-bitwise swaps & | ^ << >>, invert-bwassign the same for
# the assignment forms, invert-loopctrl swaps break and continue, remove-self-assignments drops x += y
# to x = y), with the five default operators of `make mutation` switched off, so that the two runs
# are two disjoint sets and their numbers add up. `make mutation-extended` runs them over the module
# (a quarter of an hour or more with 4 workers) into build/mutation-extended.json; PKG=model limits a
# run to one directory (the quick way to look at one area; PKG works for the other mutation targets too). Informational: there is no efficacy threshold on
# purpose, the weekly workflow runs it with continue-on-error and prints the survivors and the timed-out
# mutants in its summary.
EXTENDED_ON := --invert-assignments --invert-bitwise --invert-bwassign --invert-logical --invert-loopctrl --remove-self-assignments
DEFAULTS_OFF := --arithmetic-base=false --conditionals-boundary=false --conditionals-negation=false --increment-decrement=false --invert-negatives=false
MUTATE_EXTENDED := $(GREMLINS) --output build/mutation-extended.json $(DEFAULTS_OFF) $(EXTENDED_ON)

# gremlins does not create the directory of its report: without it the run ends with "impossible to
# write file" and the job summary says there was no report.
mutation:
	@mkdir -p build
	rm -f build/mutation.json
ifneq ($(origin EFFICACY),undefined)
	go run ./internal/mutationreport threshold "$(EFFICACY)"
endif
	go run ./internal/mutationreport coefficient "$(TIMEOUT_COEFFICIENT)"
	$(MUTATE)
ifneq ($(origin EFFICACY),undefined)
	go run ./internal/mutationreport check build/mutation.json "$(EFFICACY)"
endif

mutation-diff:
	@mkdir -p build
	rm -f build/mutation.json
ifneq ($(origin EFFICACY),undefined)
	go run ./internal/mutationreport threshold "$(EFFICACY)"
endif
	go run ./internal/mutationreport coefficient "$(TIMEOUT_COEFFICIENT)"
	$(MUTATE) --diff $(or $(BASE),origin/main)
ifneq ($(origin EFFICACY),undefined)
	go run ./internal/mutationreport check build/mutation.json "$(EFFICACY)"
endif

mutation-extended:
	@mkdir -p build
	go run ./internal/mutationreport coefficient "$(TIMEOUT_COEFFICIENT)"
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
