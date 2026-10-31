# Contributing

Issues and pull requests are welcome. Open an issue first for a new setting or a change to the exported API. This page is the checklist for **any change to the client**; tick every line that applies, or write in the pull request why it does not.

## 0. Before you start

- [ ] Run `make hooks` once per clone. The pre-commit hook runs format, vet (also `-tags live`), lint, tests with `-race` and the coverage policy, and must never be bypassed (`--no-verify`): a red hook is what CI would show.
- [ ] Work on a branch (`feat/`, `fix/`, `docs/`, `test/`, `chore/`). Nothing goes to `main` directly.
- [ ] Decide the kind of change; it drives the version ([RELEASING.md](RELEASING.md)): a change that breaks the exported API is a minor version while the module is 0.x, a new setting or a fix is a patch, tests, docs and CI do not release by themselves.
- [ ] A change that can cut management access or break traffic (the IP settings, the VLAN mode, PVIDs, link aggregation, port settings of a used port) is designed with the write gates in mind and is never exercised on a switch you cannot afford to lose. Settings that can lock a user out (credentials, reboot, factory reset, config restore, firmware) are deliberately not offered.

## 1. Code

- [ ] A setting is one file named after it (`ports.go`: types, reader, writer, parser, validation) with its tests in the file of the same name (`ports_test.go`); the file map and the naming conventions are in [docs/architecture.md](docs/architecture.md). Settings depend on the client and the shared helpers, never on each other.
- [ ] The wire layout is written down in [`PROTOCOL.md`](PROTOCOL.md) first (TLV id, byte layout, source: capture, vendor code, or measured), including what is not verified.
- [ ] Protocol numbers are named constants with a doc comment (`internal/wire` for TLV ids and sizes); no magic values and no layout bytes outside the client package.
- [ ] A domain concept is a type, not a bare `int` or `string`, and so is a bound or limit computed from several inputs (`PortVLANLimit`: the larger of the reported group count and the port count). Signs a type is missing: a method taking a bare number of another kind than its receiver, a struct built only to call one of its methods, a cached field where `0` means both "not read" and "unknown", two same-typed numbers with different meanings side by side. The type owns the rule; callers do not re-check it.
- [ ] A function takes at most about three arguments of different meaning, and never a run of same-typed ones: group them in a named struct (`PortConfig`, `StormConfig`).
- [ ] A reader returns a domain type (not TLVs). A writer validates every argument before anything is sent and returns an error that matches `ErrInvalid`.
- [ ] Every writer goes through the write gate with an `Operation{Name, Ports, Impact}`. The impact is the worst the method can do with any arguments (cosmetic, traffic or management).
- [ ] A writer reads back and compares when the switch is known to ignore values silently.
- [ ] Exported identifiers have doc comments that state preconditions and safety implications. Cancelling the context stops the call before a write.
- [ ] A parser of switch replies cannot panic on short, long or malformed data (bounds checked; a fuzz target for a new parser).
- [ ] Logging goes through the caller's `*slog.Logger` only; no `fmt.Print*` or `log.*` (the linter enforces it), never a TLV *value*, a password or a key. Attribute names are a contract: add one, never rename or repurpose one.

## 2. Tests

Coverage must stay at 100% of reachable statements (`make cover`; exclusions only with a `// coverage: ignore - <reason>` comment on the function). A test that depends on timing is a bug: build the state deterministically. After writing a test, break the code on purpose and confirm the test fails.

- [ ] Wire tests with exact bytes against the fake switch (`escptest`): the golden path, a captured vector where one exists, short or truncated replies, boundary values, invalid input (matches `ErrInvalid`), and **nothing sent** when validation or the gate refuses.
- [ ] The gate sees the right operation, ports and impact (`write_gate_test.go`); a cancelled context sends nothing.
- [ ] **The fake** (`escptest/model.go`): the stateful fake stores and answers the new setting and enforces any rule the switch has (refusals, exclusivity, derived values) **as measured on hardware**. Rules that are only assumed are marked as such in a comment. `escptest` is public API: consumers test against it.
- [ ] **Live checks** on a switch you own (`go test -tags live -run Live .`, variables `ESCP_HOST`, `ESCP_USERNAME`, `ESCP_PASSWORD`; the others are named in each test): unused port, deferred restore before the first write, restore verified. Take a configuration backup first, watch pings, and keep the uplink and ports with devices out of it. Not possible for a change? Say so in the pull request.
- [ ] `gofmt`, `go vet` (also `-tags live`), `make lint`, `make vuln` and `make cover` pass. `make fuzz` (30 s per target) when a parser changed.

## 3. Documentation

- [ ] The documentation is current: doc comments on the exported API, runnable examples with checked output (`example_test.go`, `example_fake_test.go`; they run against the fake switch) for new public entry points, the README tables and the guides in `docs/`. `make pkgdoc` renders the package pages as pkg.go.dev shows them (HTML and text under `build/pkgdoc`); read the page of what you changed.
- [ ] [`PROTOCOL.md`](PROTOCOL.md) describes the layout and its status (verified live on which firmware, vendor code only, or assumed).
- [ ] **Hardware and firmware compatibility:** the one source is [`safety/compatibility.json`](safety/compatibility.json). Record there which hardware revision and firmware build the change was verified on, then run `make compat-table` to regenerate the README table. The firmware allow-list is read from the same file: a build becomes writable without the caller's opt-in only when a row with `"support": "verified"` names it, and only after it has really been verified on a switch.
- [ ] `CHANGELOG.md` gets an entry at release time (`scripts/release.sh`), taken from the diff and not from memory; breaking changes are called out.

## 4. Pull request

- [ ] Commit messages and the PR title follow Conventional Commits (lower-case imperative subject, no trailing period). The squash commit takes the PR title.
- [ ] The PR description says what changed, what was verified (and on what), what was not, and the remaining risks.
- [ ] No passwords, MAC addresses or real addresses in code, tests or docs. Use documentation addresses (`192.0.2.0/24`).
