# Adversarial review: standing checklists

The [defect classes](defect-classes.md) ask "what went wrong before". This page asks "what would a hostile or careless person do to this design". It is read at design time and before every release. It has two checklists, one for architecture (will this survive a stranger, a second consumer and a 1.x promise) and one for security (who can reach this, what can they do, and what do we promise), and a short statement of how and when they run.

It describes classes of threat and the probes to run. It does not list weaknesses: a weakness found by a review is reported privately, see [How findings are reported](#how-findings-are-reported).

## Part 1. Architecture red team

Each item is a question to ask of the change or of the component, with the signal that it fails. Items have ids (`AR-nn`) so that a review can cite them.

| Id | Question | Fails if |
|---|---|---|
| AR-01 | **The first ten minutes.** Does the quick start in the README work as pasted, in an empty directory with only the public repository? | a snippet does not compile, needs a private file, or the first error does not say what to do |
| AR-02 | **Misuse sweep.** Call every exported function with the zero value, a nil, a canceled context, after `Close`, in the wrong order, from two goroutines, with a second client for the same device, and ignoring every error. | anything panics, hangs, or sends something it should not; or the behavior is not the documented one |
| AR-03 | **Extensible without a break.** For each exported identifier: can a field, value, method, parameter or error detail be added in 1.x? Consider: struct fields (unkeyed literals, `==` on the struct), enums (a `switch` without a default in a caller), interfaces that callers implement (a new method breaks them), positional parameters, sentinel versus typed errors (can detail be added and `errors.Is` still work), defaults and zero values, encodings that consumers persist (text, JSON, anything a consumer stores), log attribute names, and the fake's behavior. | the honest answer is "no" and the identifier is not marked experimental or unexported |
| AR-04 | **Layering drift.** Do imports still point one way? Does the surface re-implement a rule the model already owns? Does anything outside reach through an alias or an exported seam into what should be internal? | a layer imports upward, a rule exists in two places in two wordings, an exported identifier exists only for tests |
| AR-05 | **Interface bloat.** Is each interface defined by its consumer and no wider than what that consumer needs? Does any code decide behavior with a run-time type assertion? Must a test double implement more than a handful of methods? | an interface that implementers must grow, a producer-owned interface used by one consumer, a type assertion that picks a code path |
| AR-06 | **Hidden global state.** List every package-level variable, registry, `init`, `sync.Once`, shared socket and process-wide claim. Is each documented, bounded, resettable in tests without being exported, and free of effects across tests? | a global that changes behavior for the whole process or the whole test binary |
| AR-07 | **The second consumer.** What would a command-line tool hit (one call per process, exit codes that need a stable error taxonomy, machine-readable output, running beside another front end on the same host)? A long-lived service (many devices, different concurrency, claims that outlive a request)? A second device model (limits and rules baked into value types)? Another operating system or transport (socket sharing, reply paths verified on one platform)? | an assumption of the first consumer is hard-coded or only documented |
| AR-08 | **Time and scale.** What does the component cost with N calls against a silent device: retries times timeouts times calls, memory per call, serialization points, descriptors and goroutines? | the cost is unbounded or multiplies without a fail-fast |
| AR-09 | **The compatibility gate.** Is a break detected by a tool (`make apidiff`) and not by memory? Is there a written deprecation window? Can you write the migration row? | a break was found by a user, or you cannot write the row |
| AR-10 | **Safe by default.** Is the short path the safe path? What does a program that sets nothing do (a nil gate, a zero `Config`, an omitted attribute)? | the default sends a write that a careful user would want to confirm |

## Part 2. Security red team

### What the device's protocol allows, and what we do and do not defend

The switch speaks its own management protocol, which has weak cryptography by design (a static key that is public, a very small RSA modulus, no integrity protection; the password rides on every write). That is the device's, not ours, and a client cannot fix it. The security policy says so; this page does not repeat it.

- **We do not defend** against an attacker who can observe or inject traffic on the same layer-2 segment: they can read what the protocol exposes and forge what it does not authenticate. The documented remedy is the network: a trusted management segment.
- **We do defend** (these are the standing promises, and each has a test or a probe below): that the client never makes the weakness worse (random session keys, no credential in logs, errors, panics or the log attributes); that a value learned from the network is validated before it is used as a destination or an identity; that every read from the network is bounded in size, count and time; that the client talks to the device it was configured for and refuses a stranger; that a write is refused, not sent, when a gate refuses it; and that a safety gate, which reads state from replies that are not authenticated, protects against self-inflicted lockouts and is **not** a security boundary against a peer.

### Threat model: library

| Id | Threat | Asset | Trust boundary | Defended by | Not defended | Standing probe |
|---|---|---|---|---|---|---|
| TM-01 | An observer on the segment | credentials, configuration | host and device on one segment | the documentation (trusted segment) | everything the protocol exposes | none: state it |
| TM-02 | A peer that injects or races replies | integrity of reads and acknowledgements | network to client | source, sequence and identity checks; fail-closed identity | forgery by a determined peer | what does a gate or a caller decide from a reply, and is that stated? |
| TM-03 | A device other than the configured one answers (first contact, lookup, a moved address, no MAC, zero MAC, another MAC) | credentials, the right switch | network to client | pinned and learned identity, claim, validated destination | what the protocol does not authenticate | answer first, answer with every identity variation; record which bytes go where |
| TM-04 | A malformed or hostile reply (a compromised or buggy device) | the host process | network to parser | bounds, fuzz targets, no panic on short or long input | n/a | fuzz every parser; replies at the size, count and time limits |
| TM-05 | Resource exhaustion by a peer or by configuration | host availability | network and caller to client | caps on bytes and packets, a deadline per attempt and per call, bounded retries and timeouts, descriptors released | a flood that only burns CPU until a deadline | a peer that never stops; N concurrent calls against a silent device; descriptors and goroutines before and after |
| TM-06 | Credentials escaping | the admin password | client to logs, errors, panics, files | the `Password` type, which prints redacted under every verb, log attributes without values, no files written by the library | a caller that logs its own configuration | print every type that can hold a credential with every verb and handler; run a call at debug level and search the output |
| TM-07 | Guard bypass by misuse or ordering | the device's management path | caller to client | the settings table, the gate on every write path, validation before the gate | a caller that installs no gate (the default) | each writer through each gate combination; a write between a gate's read and the write; `Close` in the middle |
| TM-08 | Another process on the same host | replies, the shared reply port | host | documentation; the one-client rule inside a process | other processes, other users | two processes; two clients; a client beside another front end |
| TM-09 | The fake used outside tests | n/a | test code to production | binds loopback addresses only | misuse by a caller | search the fake for any bind that is not loopback |
| TM-10 | Supply chain: a dependency, an action, the build, the release | every consumer | upstream to repository to release | few dependencies; actions pinned to SHAs; automated dependency updates; vulnerability scans on a schedule; code scanning; minimal workflow permissions; a release workflow that re-runs the checks on the tagged tree and has the module proxy and a fresh module confirm the version | a compromised maintainer account | fork-PR workflow edits; a tag pushed by a writer; a `replace` in `go.mod`; the release workflow's secrets and who can trigger it |
| TM-11 | Untrusted input reaches a front end (a command line, a configuration from a pull request) | credentials | front end to library | the front end's job: the destination a configuration names receives the credential | a front end that runs untrusted configuration with credentials in the environment | does any front end run configuration from a source you do not trust, with credentials present? |
| TM-12 | Documentation and examples as a vector | users' habits | docs to users | examples use environment variables and documentation addresses | users who copy a literal password | search examples for literals, for `Config` printed, for real addresses |

### Probes that apply to every change

1. **The hostile peer.** For any code that reads from the network, write the test with a fake that answers first, never stops, sends the extreme of every field, replays and mixes up identities. Assert the time, memory and descriptors, and what the client does next.
2. **The secret trail.** For any code that touches a credential, print every value on the path with every formatting verb and logging handler, at debug level, with a panic and with an error.
3. **The ordering attack.** For any guard, put the write between the guard's read and the write; put `Close`, a cancel and a second caller there.
4. **The pipeline.** For any workflow, release or dependency change, ask who can trigger it, with which secrets, from which ref, and whether it can be run again for an older tag.

## Part 3. How it runs

- **Who.** A reviewer who did not write the change, in an adversarial stance, and the design reviewer for Part 1. The security pass does not need the diff to be understood: it attacks the component through the threat table.
- **When.**
  1. *Design review*, before implementation, for any change to an exported identifier, a documented behavior, an architectural boundary, a write path or network-facing code: the items of Part 1 and the rows of Part 2 that the change touches.
  2. *Pull request review*, for a change that touches parsing, credentials, logging, a gate, a workflow or a dependency: the rows named by the files.
  3. *Before every release*: both parts over the release's diff and over the component, plus the adversarial probe of each [defect class](defect-classes.md) against the functions added in the release.
  4. *After a dependency, workflow or release-process change*: TM-10 again.
- **What it produces.** A list of findings with an id (the `AR` item or the `TM` row), the scenario, a proof (a test against the fake or a command and its output; "from reading" says so and ranks lower) and a suggested fix. A finding that shows a question was missing adds an item here or an entry to the defect classes in the same change.

### How findings are reported

- **Anything exploitable is reported privately**, through the repository's private vulnerability reporting (see [SECURITY.md](../SECURITY.md)). It is never written in a public issue, pull request, comment or commit message. A fix for it can be described publicly as a robustness change, without the attack.
- A public issue may describe the **class** (a defect-class entry) and the fix that closes it, not a way to exploit a deployed version.
- Design findings (a break at 1.0, a trap in the surface) are public issues with the `breaking` label where they apply.

The Terraform provider has its own additions to both checklists (schema, state, plan and apply, the registry); they are in that repository and apply on top of this page, which covers the library underneath.
