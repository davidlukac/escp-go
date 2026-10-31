# Releasing

How a version of the library is cut and published. Maintainers only.

## Conventions

- All changes go through a pull request: branch, commit, push, wait for green CI, **squash merge**. Nothing is pushed to `main` directly.
- Commit messages and PR titles follow [Conventional Commits](https://www.conventionalcommits.org/): `feat:`, `fix:`, `docs:`, `test:`, `refactor:`, `ci:`, `build:`, `chore:`; `!` or a `BREAKING CHANGE:` footer marks an API break.
- **Versions** ([SemVer](https://semver.org/)): while the module is 0.x, a change that breaks the exported API is a **minor** version and everything else a patch; the CHANGELOG entry names each break. From 1.0.0 a break needs a new major version, and a major version 2 or later changes the import path (`.../escp-go/v2`). `make apidiff` shows what changed since the latest release.
- **What counts as API:** every exported name, the error values and their meaning, the `Speed`/`QoSMode`/`VLANMode` spellings, the log attribute names and the levels of existing messages (ADR-0007, summarised in the README), and the behaviour of `escptest`.
- A tag is **immutable**. The Go module proxy caches a version for good; a bad release is fixed by a new patch version and, if it must not be used, a `retract` directive in `go.mod`. Never move or delete a pushed tag.

## Cutting a version

1. On a branch: `scripts/release.sh X.Y.Z "what changed for users"` adds the `CHANGELOG.md` entry, runs `make cover lint vuln` and commits only the CHANGELOG. For a release that changes how a setting is written to a switch, also run the live tests (`go test -tags live`, see the README) on a test switch and say so in the PR.
2. Open the PR, wait for CI, squash merge.
3. On `main`, once merged and pulled: `scripts/release.sh --tag X.Y.Z` tags the merge commit locally. Push **that one tag** (the owner's go-ahead is needed: the push is what publishes).
4. The tag push starts `release.yml`, which does the rest: it checks that the tag is a version on `main`, re-runs `make cover lint vuln` on the tagged tree, creates the GitHub release with the CHANGELOG section as its notes, waits until `proxy.golang.org` serves the version, installs it in a fresh module and builds a program against `escp`, `safety` and `escptest`, and asks pkg.go.dev to index it. Watch it: `gh run watch`. To repeat it for an existing tag: `gh workflow run release.yml -f tag=vX.Y.Z` (every step is safe to repeat).

## Publishing

There is no registry upload. The tag on the public repository is the release, and `release.yml` proves it is installable. By hand, the same checks are:

- `GOPROXY=https://proxy.golang.org go list -m github.com/davidlukac/escp-go@vX.Y.Z` (caches the version in the proxy).
- In a scratch module: `go get github.com/davidlukac/escp-go@vX.Y.Z` and build something that imports the packages.
- `curl -X POST https://pkg.go.dev/fetch/github.com/davidlukac/escp-go@vX.Y.Z` asks pkg.go.dev to index it; the page shows the documentation once the license is detected (MIT).
- Consumers (the provider, the command line client) bump with Dependabot or `go get`.

## Local development with a consumer

A consumer checkout next to this one (`../terraform-provider-tplinkeasysmart`) can build against the working tree with a local, untracked `go.work` (`go work init . ../consumer`). CI and releases never see it; a consumer's `main` requires a **released** version, never a pseudo-version.
