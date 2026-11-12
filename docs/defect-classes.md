# Defect classes

The classes of defect that reached a release of this library, and for each one the question to ask before writing code, the test to write, the check that watches for it, and the probe a hostile or careless caller would use. They were found by independent reviews and by a review of the fixes together (see the issues named in each entry); every one was findable earlier, by a question or a test.

How to use this page:

1. Before you write code, find your change in the routing table, read the entries it names, and answer each DESIGN question in the issue or the pull request (a sentence is enough; "not applicable" is an answer).
2. While you write tests, satisfy each TEST requirement of those entries.
3. When you review, try the ADVERSARIAL probe of each entry the change touches.
4. When a defect is found (by a user, a review, CI or a later change), name its class by id in the fix. If no class fits, add one here in the same pull request, or say in the fix why the existing class's check missed.

`DC-C` entries apply to every Go repository of this project (the provider links to them from [its own catalog](https://github.com/davidlukac/terraform-provider-tplinkeasysmart/blob/main/contributing/defect-classes.md)); `DC-L` entries are specific to this library. The security and architecture checklists that go beyond defect classes are in [adversarial-review.md](adversarial-review.md).

## Routing: which entries does my change touch?

| My change | Read |
|---|---|
| Any change | [DC-C01](#dc-c01) (promises), [DC-C05](#dc-c05) (tests), [DC-C08](#dc-c08) (hygiene) |
| Adds or changes a call that can block (a lock, a retry, a lease or settle wait, a lookup, a read of fragments), or `Close` | [DC-L01](#dc-l01), [DC-L02](#dc-l02), [DC-L03](#dc-l03), [DC-C06](#dc-c06) |
| Decides that something is absent, empty or silent from a timed read (a drain, a probe, a "no answer" check) | [DC-L09](#dc-l09), [DC-L01](#dc-l01), [DC-C06](#dc-c06) |
| Adds or changes a writer, a gate, an `Operation` or an impact class | [DC-L02](#dc-l02), [DC-L03](#dc-l03), [DC-L05](#dc-l05), [DC-L07](#dc-l07), [DC-L08](#dc-l08) |
| Adds a parser, a decoder, or uses anything read from the network | [DC-L05](#dc-l05), [DC-L06](#dc-l06), [DC-L04](#dc-l04) |
| Changes how a switch is found, named, moved or claimed, or what a client does after a refusal | [DC-L04](#dc-l04), [DC-L01](#dc-l01), [DC-L02](#dc-l02) |
| Adds or changes an exported identifier, a `Config` field, a default or a zero value | [DC-L08](#dc-l08), [DC-C03](#dc-c03), [DC-L05](#dc-l05) |
| Changes behavior that code outside the function relies on (another package, the provider, an example, a script) | [DC-C03](#dc-c03), [DC-C04](#dc-c04) |
| Touches a function that another open or recent change also touches | [DC-C04](#dc-c04) |
| Writes a sentence of documentation that promises something, a table, a count or a limit | [DC-C01](#dc-c01), [DC-C02](#dc-c02), [DC-C07](#dc-c07) |
| Adds a test, a fake switch rule or a test seam | [DC-C05](#dc-c05), [DC-C06](#dc-c06) |
| Touches a workflow, the release, a dependency, an example, a log line or an error text | [DC-C08](#dc-c08), [DC-C09](#dc-c09) |

## Entry format

Each entry has the same fields, in this order: the rule in one sentence; why (what happened); the DESIGN question; the TEST requirement; AUTOMATION; the ADVERSARIAL probe.

AUTOMATION names each check by kind and name, so that a test can verify that the name exists. The kinds are `test:` (a test function), `fuzz:` (a fuzz target), `make:` (a Makefile target), `lint:` (a linter enabled in `.golangci.yml`), `analyzer:` (a directory of `devtools/`, or the name an analyzer declared there carries), `script:` (a path), `workflow:` (a file in `.github/workflows/`), `reading:` (it cannot be scripted; the reason follows) and `proposed:` (not built yet; the name is followed by the issue that tracks it in parentheses; #195 tracks the ones that have no issue of their own). A claim that a check exists without a name is not accepted. `make lint` runs `devtools/catalogcheck` (`make catalogcheck`), which fails with one line per problem when a name in this page does not resolve (a renamed test, a removed target), when a `reading:` has no reason or a `proposed:` no issue, when a `proposed:` names something that exists already (write it with its kind), when a field of an entry is missing or out of order, when an entry is not in the routing table, and when an id or a link to this page written anywhere else in the repository resolves to nothing.

---

## Common classes (both repositories)

<a id="dc-c01"></a>
### DC-C01 Every documented guarantee has a test

**Rule.** A sentence in a doc comment, guide, README or schema description that promises a behavior ("sends nothing", "returns in moments", "never repeated", "safe on any firmware", "compiles") has a test that fails if the sentence were false, or the sentence is removed.

**Why.** The documentation of `Close` and `ErrClosed` promised that every later call "returns `ErrClosed` and sends nothing" and that a call `Close` cut short returns it too; the combination of the fixes showed that was false on five paths, and a closed client could re-open its socket and send (#181). "A failed key exchange before the write is only `ErrNoReply`" was false after an unanswered first send (#183). Five of ten Go blocks in the guides did not compile (#175).

**Design question.** Which sentences will the documentation of this change contain: what it does, never does, how long it takes, what it sends, what it returns? For each, what would a test observe from the outside?

**Test requirement.** One test per promise, observing it from outside: datagrams the fake received (not what the code intended to send), elapsed time against a bound with a deliberately blocked dependency, `errors.Is` and `errors.As` on the result. "Sends nothing" asserts the fake's count; "returns in moments" asserts a bounded wait; "never" has a test that tries to make it happen.

**Automation.** `test:TestEveryGoBlockOfTheDocumentationCompiles`, `test:TestEveryErrorIsInTheErrorsGuide`, `test:TestEveryLoggedAttributeIsInTheLoggingGuide` (three kinds of claim that are checked today); `analyzer:guarantee` (`make guarantee`, part of `make lint` and blocking: each exported identifier whose doc comment states a guarantee must be matched with a `// guarantee: <Name>` marker in a test of its package, or in a test of another package of the module with the package in front of the name, `// guarantee: model.WriteGate`; a new documented guarantee therefore comes with a test that fails when the sentence is false, or the sentence is not written; the scan reads the wording of the sentence, so a sentence that is no promise is reworded and not exempted); `reading:` deciding which sentence is a guarantee needs a person.

**Adversarial probe.** Read each doc comment of the identifiers you touched and write the call that makes it false: cancel in the middle, `Close` in the middle, two callers at once, a lost reply, the zero value, a nil context.

<a id="dc-c02"></a>
### DC-C02 One source per fact

**Rule.** A fact (a name, a count, a list, a default, a limit, a layout, a support level) is written in one place; every other place is generated from it or is checked against it by a test that fails when it drifts.

**Why.** After the values moved into package `model` (#113), the README package table, the architecture guide, the package documentation and comments still named the old package, and the protocol notes contradicted themselves about multi-type reads (#175). The architecture guide's file list was checked in one direction only.

**Design question.** Where does this fact already live? If it changes, which places say it? Can the new copy be generated from the one source?

**Test requirement.** A test that fails when a copy drifts: a generated table compared with the file, snippets compiled, a list compared with the code in both directions (nothing missing, nothing extra).

**Automation.** `test:TestTheGeneratedTablesAreCurrent` and `make:settings-docs` (settings table, impact classes, validators); `test:TestReadmeCompatibilityTable` and `make:compat-table`; `test:TestEveryGoBlockOfTheDocumentationCompiles`; `lint:misspell`; `analyzer:catalogcheck` (every name this page gives to a check, and every id of it and link to it written elsewhere in the repository, resolves); `proposed:stalenames(#195)` (a script that lists, for every exported identifier removed or renamed since the last tag, each place in the documentation that still uses it).

**Adversarial probe.** Rename an identifier or change a limit and search every place that states it; each place that stays green is the finding.

<a id="dc-c03"></a>
### DC-C03 A changed default or behavior lists its consumers

**Rule.** A change to a default, a zero value, what "empty" means, an error that used to be returned, or any behavior callers relied on lists its consumers in the pull request (callers in this repository, other repositories, test harnesses, scripts, examples, documentation) and names the ones the change does not touch.

**Why.** Making relocation by MAC opt-in (#144) was right, but three consumers relied on the old default and only a run on hardware showed one of them (provider#191). Making `Close` final (#139) broke a downstream test harness that relied on a closed client opening again (provider#188). Replacing the exported `Firmware.Build` with a method (#167) broke a downstream gate at compile time.

**Design question.** What does the old behavior do for someone who changed nothing? Who depends on it, by search and not by memory? Is there a deprecation path, a flag or a migration row?

**Test requirement.** A test in each consumer that fails on the old assumption; the migration row in the changelog names the issue and what a user does.

**Automation.** `make:apidiff` (signatures only); `proposed:consumers(#195)` (a script that searches the known consumer repositories, examples and docs for each identifier an API diff removes or changes); `proposed:canary(#195)` (a CI job in the provider that builds and tests it against this library's `main`); `test:TestTheConfigDefaultsAreTheSnapshot` (renders every default and limit of `Config`, and every duration of the package, to `testdata/defaults.txt`, so a flipped default shows in the diff of its pull request).

**Adversarial probe.** For each changed exported identifier or default, search all repositories and the docs for its use; flip the default and run every other suite.

<a id="dc-c04"></a>
### DC-C04 A change that is right alone is checked with its neighbors

**Rule.** Changes that touch the same functions or the same documented behavior are read and tested together before a release, and a fix for a class is checked for the same class in its own new code.

**Why.** Fifteen fixes each passed CI; reading them together found 18 more defects (#181, #183). The fix that made `Close` end calls in flight left four waits that hold the lock blind to it; the typed outcome of an unconfirmed write regressed management writes on a forgotten session.

**Design question.** Which functions does this change touch that another open or recently merged change also touches? What property of the combination must still hold?

**Test requirement.** A test per property of the combination, written against the merged tree, not the branch.

**Automation.** `make:apidiff`; `proposed:seams(#195)` (a script that lists functions changed by more than one pull request in a release range, with the pull requests); `reading:` the combined diff of the release, read as one change.

**Adversarial probe.** Take the classes the release fixed; run each class's probe against every function the release added.

<a id="dc-c05"></a>
### DC-C05 Tests must fail when the code is wrong

**Rule.** Every behavior is pinned by a test that fails against a named broken variant; coverage and a mutation score are evidence, not proof; the fake switch is checked against something other than the code under test.

**Why.** Flipping `claimsByDefault` to false passed the whole suite, because the test package set the same global for every test (#146). A constant session key passed (#173); four settle and move conditions had no test (#173); a fake that does not bind a session to an address cannot fail a test of the session reset. The mutation tool's default operators are five; none removes a call, changes a constant or swaps arguments. Hand mutation of the library and the provider found 19 survivors in 114 mutants while the tool reported full efficacy.

**Design question.** For each line I add, what is the broken variant (inverted condition, dropped call, swapped arguments, changed constant)? Which test fails for each? Does any test seam change a production default for a whole test binary?

**Test requirement.** A test that fails for each such variant, shown by running it against the variant. A seam is an option of the test, never a global flipped in `TestMain`. A rule in the fake carries its source (a captured vector, a measurement, or "assumed") and one test that does not share the fake's assumption.

**Automation.** `make:mutation-diff`, `make:mutation`, `make:cover`; `analyzer:subtests`; `proposed:gremlins-operators(#195)` (enable `invert-logical`, `invert-assignments`, `invert-loopctrl` in a `.gremlins.yaml`); `proposed:handmutants(#195)` (a script that applies a committed list of hand mutants and requires each to fail a test); `proposed:testseam(#195)` (an analyzer that flags a `TestMain` or `init` that assigns a package variable of production code).

**Adversarial probe.** Mutate by hand: drop each call, flip each constant, swap each pair of arguments, delete each `if`; run only the package's tests.

<a id="dc-c06"></a>
### DC-C06 Tests do not depend on time, order, ports or the machine

**Rule.** A test builds its state deterministically: no wall-clock wait for something that must not happen, no process-global resource without a release, and the suite passes shuffled, repeated, under `GOMAXPROCS=1` and under load.

**Why.** Flaky tests recurred in four releases: a discovery test that expected a reply inside 200 ms (#78), a port test that counted the kernel's draws (#86), one that depended on the ports a Windows runner forbids (#101, #71), a registry test that waited 200 ms for a reply it never needed (#137). The fake held every port it handed out for the life of the process and ran out of descriptors at about 7,600 ports (#171).

**Design question.** What does this test wait for? What resource does it hold, and who releases it?

**Test requirement.** Synchronize with channels and contexts, never with a sleep; release with `t.Cleanup`; ports from an allocator that gives them back; a deterministic clock or a bounded wait for anything timed. Where the fake is muted so that a call parks in its wait, a wrapper around the client's own socket (the package's `datagramConn` seam; `signalingConn` in `cancel_close_table_test.go` is one) that signals when the call has reached the read is an acceptable way to know that the call is waiting, and replaces a sleep.

**Automation.** `make:soak` (weekly: shuffled, repeated, with the race detector, on three operating systems), in three conditions that `internal/soak` defines once: `make:soak-idle`, `make:soak-single` (`GOMAXPROCS=1`) and `make:soak-loaded` (busy goroutines on every CPU beside the tests); `workflow:weekly.yml`; `proposed:nosleep(#195)` (a `forbidigo` pattern for `time.Sleep` in tests, with a reason-comment escape).

**Adversarial probe.** Run `go test -race -shuffle=on -count=5`, with `GOMAXPROCS=1`, with a CPU hog beside it, and with two copies of the binary at once.

<a id="dc-c07"></a>
### DC-C07 A support or platform claim names what was verified

**Rule.** Statements such as "verified on", "works on any firmware" or "tested on Windows" name the exact device, firmware, operating system and test; the compatibility file is the one source, and nothing broader is written in prose.

**Why.** The compatibility row for one firmware build said more than had been verified on it, and the provider's firmware gate inherited the claim (#179). A doc comment said a write was "safe on any switch" while the gate refused it on an unverified build.

**Design question.** What did I actually run this on? What will a reader assume I ran it on?

**Test requirement.** The gate and the README table read the same file; a test fails when the README differs; a hardware claim cites the test that exercised it.

**Automation.** `test:TestReadmeCompatibilityTable`, `make:compat-table`; `reading:` whether a claim is broader than its evidence needs a person who knows what was run.

**Adversarial probe.** Pick each claim and ask which command produced its evidence, on which device.

<a id="dc-c08"></a>
### DC-C08 Nothing private, nothing secret, one spelling

**Rule.** No credential, private address, MAC address, host name or reference to a maintainer's private workspace reaches code, tests, documentation, issues, pull requests or release notes; a secret is redacted on every path a value can be printed; text is American English.

**Why.** Real addresses of a private network shipped in documentation and examples of releases (provider#158). A spelling check that ran only in CI let a British word reach a pull request (#99). A test inherited the environment of the git hook that ran it and acted on the repository being committed to (#103).

**Design question.** Which values does this change add to examples, tests, logs and errors; is any of them real? Which types hold a secret, and how does each print?

**Test requirement.** Documentation values only (`192.0.2.0/24`, `00:00:5e:00:53:xx`); a secret-absence test over the log output of a call that carries a credential; a type that holds a secret prints redacted under every verb, JSON and `slog`.

**Automation.** `lint:misspell`, `make:spell`, `test:TestTheTextIsAmericanEnglish`, `test:TestNothingPrivateToAWorkspaceIsPublished`, `lint:gosec`; `proposed:secretformat(#195)` (an analyzer that requires `String`, `GoString`, `Format` and `LogValue` on a type with a field named like a credential).

**Adversarial probe.** Print every type that can hold a credential with every formatting verb and logging handler; search the diff and the generated documentation for addresses, MACs and host names.

<a id="dc-c09"></a>
### DC-C09 The pipeline is repeatable, pinned and protected

**Rule.** A workflow can be run again for any tag we support, third-party actions are pinned to commit SHAs, permissions are the least needed, and a step that signs or publishes runs only from a protected ref; a red scheduled or release run is a defect.

**Why.** Running the release workflow again for a tag from before an example program existed failed (#176), and its fresh-module check called a function that had moved (#131). A burst of merges canceled the runs on `main` and the badge read as failing (#94).

**Design question.** What runs on a tag, with which secrets, triggered by whom, and can it be run again?

**Test requirement.** A dry run in CI for every publishing step (a snapshot build, an install check); every workflow linted.

**Automation.** `make:actionlint`, `workflow:test.yml`, `workflow:release.yml`, `workflow:weekly.yml`, `workflow:codeql.yml`; `make:modcheck` (`go mod tidy -diff` and `go mod verify`, for the module and for `devtools`) and `make:actionlint`, both run by `workflow:test.yml` on every pull request; `reading:` that every checkout sets `persist-credentials: false` and that a workflow's permissions are read-only unless a job writes is checked by a person at review, no check fails when a new workflow omits them; `proposed:settingscheck(#195)` (a script that reads the repository's rulesets and required checks through the API and compares them with a committed expectation).

**Adversarial probe.** A pull request from a fork that edits a workflow; a tag pushed by someone with write access; a compromised dependency (TM-10 of the [adversarial checklist](adversarial-review.md)).

---

## Library classes

<a id="dc-l01"></a>
### DC-L01 Every wait and every client state obeys `Close` and the context

**Rule.** Any call that can block (a lock, a lease or settle wait, a lookup, a fragment read, a retry) ends promptly when the caller's context ends or the client is closed and returns the sentinel that says why; a client in any state other than open (zero, closing, closed, refused) sends nothing and never panics.

**Why.** `Close` ended only the exchange: the lease wait, the move wait, the settle after a lost reply and the lookup by MAC ignored it, so four calls took seconds, not "moments", and returned the wrong error (#181); a client closed between the read and the write of `SetIPConfig` could re-open its socket and send (#181). A lock that ignored its context kept a canceled caller waiting 3.8 s (#93). The firmware gate held its lock across a network read (#153). The zero `Client` panicked (#161).

**Design question.** List every place this change can wait: lock, I/O, poll, sleep, channel. For each: who can end the wait (context, `Close`, deadline), and what does the caller get? Is a lock held across it, on purpose, and is that written down?

**Test requirement.** For each blocking call, a cancel test and a `Close` test against a fake that never answers: it returns within a bound, `errors.Is(err, ErrClosed)` (or the context's error) holds, and the fake received no datagram after `Close` began.

**Automation.** `test:TestCloseEndsTheCallInFlightAndTheCallsWaitingBehindIt`, `test:TestAWriteCutShortByCloseReturnsErrClosed`, `test:TestUsedAndClosedClientsLeaveNoGoroutinesBehind`, `lint:contextcheck`, `lint:noctx`; `test:TestEveryMethodEndsWithItsContextAndClose` (reflects over every exported method of `Client` that takes a context and requires a row for it in a table of cancel and `Close` cases, against a muted fake; a method without a row, and a row without a method, fail); `analyzer:lockwait` (flags a blocking operation while a mutex is held; an intended wait carries `// lockwait: <reason>`); `proposed:waitcheck(#195)` (an analyzer that flags a blocking call in a function that takes a context and never selects on it); `reading:` whether holding a lock across I/O is intended.

**Adversarial probe.** Call every method against a muted switch and cancel or `Close` at a random moment; after `Close`, assert no goroutine, no socket and no datagram. Call every method on the zero value, with a nil context, after `Close` and during `Close`.

<a id="dc-l02"></a>
### DC-L02 Say whether anything was sent

**Rule.** Every path that reports the outcome of a write states whether the request was sent: a refusal before the send says nothing was sent; an unknown outcome is its own typed error that survives every later step (re-key, settle, retry) and is never matched by a known outcome.

**Why.** A write that got no reply was reported as "no reply" and sent again, although the switch may have applied it (#149). After the fix, a call canceled before its write was reported as unconfirmed, a failed re-key dropped the typed error, and a settled "not applied" still matched the unconfirmed sentinel (#183).

**Design question.** For each exit of the write path (refused by validation, refused by the gate, canceled before the lock, canceled after the send, re-key failed, settle failed): was a datagram sent, what does the error say, which sentinels match?

**Test requirement.** A table of fault and exit against (sent or not, the error identity through `errors.Is` and `errors.As`, writes the fake applied), using the fake's `DropSets`, `RSADrop` and cancellation.

**Automation.** `test:TestAWriteTheSwitchNeverAnswersIsUnconfirmedAndSentAsOftenAsItsImpactAllows`, `test:TestEveryWriteMethodDeclaresItsOperation`, `test:TestOnlyKnownPlacesSendASET`; `proposed:outcometable(#195)` (a generated table test over the fake's fault list and every exit); `reading:` whether a `sent` flag is set on every return needs a person.

**Adversarial probe.** Cut a write at every step (before the gate, before the lock, after the send, in the middle of a re-key, in the middle of a settle) and compare the reported outcome with the fake's write count.

<a id="dc-l03"></a>
### DC-L03 Decide and write in one critical section

**Rule.** A guard or a read-modify-write that reads state and then writes it holds one lock, or uses a compare-and-set, across both; no other caller can change the state between the check and the write.

**Why.** `SetHostname` read the management settings, released the lock and wrote them back, which undid a concurrent `SetIPConfig` in about one run in six when a gate read first (#141). Two limit guards had the same shape and were found only when the fixes were read together (#183).

**Design question.** What does this call read before it writes? What can another caller change in between? Does the critical section cover the whole read and write, including the gate? A discovery read counts as the read: `SetHostname` reads the address settings with `Info` (a discovery probe) and writes them back, so it is a read-modify-write like one that reads a setting page.

**Test requirement.** A test that parks the first call between its read and its write (a gate or a hook), completes a second call, releases the first and asserts the final state; it must fail against the code without the lock.

**Automation.** `test:TestReadModifyWriteCallsDoNotUndoEachOther`, `test:TestReadModifyWriteCallsGiveUpWhenTheirContextEnds`, `make:soak` (race detector); `analyzer:readwrite` (flags a read method of the client followed by a write method outside the modify guard; an intended case carries `// readwrite: <reason>`); `proposed:readsfirst(#195)` (a column in the settings table naming the methods that read before they write, and a test that each appears in the atomicity test); `reading:` finding a read hidden in a helper.

**Adversarial probe.** Run each pair of write methods at once against a gate that reads and a hook that pauses between read and write; the final state must equal one of the two serial results.

<a id="dc-l04"></a>
### DC-L04 Know whose switch it is, and who owns the client

**Rule.** A client's identity (the pinned MAC, the learned MAC, the address) is established before anything with credentials is sent, compared on every reply and claimed for the process; a client that lost its claim or met the wrong device sends nothing further, and no way of naming a switch (address, MAC, relocation) evades the claim.

**Why.** A client without a pinned MAC wrote to a device that took over the address (#80). A reply without a MAC became the identity of the switch (#108, #155). A client made with an address and one made with a MAC could both talk to one switch (#151). A refused client still sent its key exchange and evicted the session of the client that held the switch (#181). The firmware gate remembered one verdict per gate and not per device (#109).

**Design question.** In how many ways can this switch be named, and what is the claim key of each? What is the identity before the first reply, after it, after a move? What does a client that lost the claim do on its next call?

**Test requirement.** Two clients through every pair of naming ways are refused; a refused client's second call sends nothing; first-contact variations (no MAC, zero MAC, another MAC, the right MAC from another address) each have a test.

**Automation.** `test:TestAClientByAddressAndAClientByMACOfOneSwitchAreRefused`, `test:TestASecondClientOfTheSameSwitchIsRefused`, `test:TestARefusedClientHoldsNothing`, `test:TestAFirstReplyWithoutAMACDoesNotBecomeTheIdentity`, `test:TestAReplyWithoutAMACIsRefusedWhenThereIsAnIdentity`; `proposed:claimmatrix(#195)` (one table test over every pair of naming ways and every state of the first contact).

**Adversarial probe.** Dial the same switch by every spelling and in every order; answer with another device, with no MAC, with a zero MAC, with the right MAC from another address; record which bytes go to whom.

<a id="dc-l05"></a>
### DC-L05 One rule at every entry

**Rule.** Each value is validated once, by its own type (`Validate` or `Parse`), and every entry (a caller's argument, a decoder for JSON or text, a parser, the configuration, a value learned from the network) uses that rule, so no entry accepts what another refuses; an empty collection or state the caller owns means "none", and only a patch-style call may refuse an empty list.

**Why.** Input validation was fixed one case at a time in seven consecutive releases: `ParsePortID` accepted "+3" (#53), `ParseMAC` accepted the all-zero address (#90), host spellings the system resolver reads as broadcast passed (#81), `Timeout` and `Retries` were unbounded (#107), management addresses in reserved ranges passed (#163), an empty `SetPortPriorities` sent a full rewrite (#169). Decoders of several types still accept what their `Validate` refuses.

**Design question.** Which inputs does this change add? For each: its type, its `Validate`, every place it enters (argument, JSON, text, configuration, wire), and its zero and empty meaning.

**Test requirement.** For each type, a table of valid and invalid values at the boundaries, including the reserved and special ranges of a value that names an address (unspecified, "this network" 0.0.0.0/8, loopback, multicast, link-local, 240.0.0.0/4 with the limited broadcast, and the network and broadcast address of the subnet: a validator that accepts one as a management address is the defect, as in #163), a round trip (`Marshal`, `Unmarshal`, `Validate`), "decode accepts only what `Validate` accepts", nothing sent when refused, and hostile strings (a NUL, 4 MB, a leading "+", full-width digits, bidirectional controls, zero).

**Automation.** `test:TestEveryRefusedArgumentIsErrInvalid`, `test:TestEveryNamedValueReadsBackFromItsText`, `fuzz:FuzzParseMAC`, `fuzz:FuzzSettingParsers`; `proposed:TestDecodersAgreeWithValidate(#195)` (reflect over every exported type with `Validate` and `UnmarshalText`: any input the decoder accepts must satisfy `Validate`); `proposed:validatecall(#195)` (an analyzer that requires an exported function taking a type with `Validate` to call it before use).

**Adversarial probe.** Feed every entry the same hostile list (signs, leading zeros, huge, negative, NUL, Unicode digits, spellings the resolver accepts) and compare what each entry does with the same value.

<a id="dc-l06"></a>
### DC-L06 Bytes from the network are hostile, in size, count and time

**Rule.** Anything read from the network is bounded (bytes, packets, time per attempt and per call), is parsed by code that cannot panic on short or long input, and is never used as a destination, identity or decision before it is validated; a missing bound is a defect even when "the switch never does that".

**Why.** A fragmented reply had a deadline per packet and no packet cap, so a call outlived its timeout (#157). The address a lookup returns is checked before the client follows it (0.9.0). Stale replies left in a socket were taken for the answer to the next request (0.32.0 of the provider).

**Design question.** What does a peer control in this message? What is the largest, slowest and most numerous input we accept, and what stops it? What do we do with a value the peer chose?

**Test requirement.** A fuzz target for each parser with its corpus committed; a hostile-peer test (a fake that never stops sending, sends oversized replies, wrong source or wrong sequence); a test that a call's time stays within its budget against such a peer.

**Automation.** `fuzz:FuzzAcceptDatagram`, `fuzz:FuzzSettingParsers`, `make:fuzz`, `lint:forcetypeassert`, `lint:gosec`; `test:TestEveryParserHasAFuzzTarget` (finds every function or method that is named `parse*`, `decode*` or `unmarshal*` (or is on a short list of others) and takes bytes, a string or a TLV list, and requires a `Fuzz` function that reaches it, or an entry with the reason why not that a person reads at review); `proposed:g115(#195)` (replace the blanket gosec G115 exclusion with checked conversions or a reason per site).

**Adversarial probe.** A peer that answers first, answers forever, answers with every field at its extreme, or replays an old reply: measure time, memory and descriptors, and what the client does next.

<a id="dc-l07"></a>
### DC-L07 The declared impact is what goes on the wire

**Rule.** Every write is classified by what it can do, including what it also re-sends, declares its `Operation` (method, ports, impact, scope) from the single settings table and passes the gate; no write path, even one that is a read for the caller, bypasses the gate silently; a gate's documentation says what it does not cover.

**Why.** `SetHostname` was "cosmetic" although its request re-sends the address settings (#141). The VLAN mode change told the gate it named one port while it isolates every port (#165). Port and VLAN writes were first classed as traffic and became management in 0.4.0. The configuration download is a write on the wire exempt from the gate, which the documentation now says.

**Design question.** What does this write send on the wire (every TLV, including values read back and re-sent)? Which `Operation` does it declare? Which gate answers which question (which ports, which kind of change)?

**Test requirement.** A table over all writers: operation name, ports, impact, scope; a gate refuses and a gate allows, each with the fake's write count; nothing sent on a refusal.

**Automation.** `test:TestEveryWriteMethodIsInTheGateTable`, `test:TestEveryWriteMethodDeclaresItsOperation`, `test:TestOnlyKnownPlacesSendASET`; `proposed:TestWritersSendOnlyWhatTheirRowSays(#195)` (record the TLV types each writer sends and compare them with its settings row); `proposed:writerdiscovery(#195)` (find writers by their use of the single send function, not by a name prefix); `lint:depguard` (rules `model-layer`, `safety-layer` and `no-fake-in-production`: the value and gate packages import only what the architecture guide lists, so never the client package, and production code never imports the fake), `lint:forbidigo` (a socket is written only in `transport.go` and `discover.go`, and `time.After` is banned).

**Adversarial probe.** For each writer, send the most dangerous valid argument through every gate combination and compare the bytes the fake received with the declared impact.

<a id="dc-l08"></a>
### DC-L08 Exported means promised

**Rule.** Everything exported can be extended without a break: no exported field beside hidden state it must agree with, no bare string or number for a closed set, no positional train of arguments, no interface that implementers must grow, defaults and zero values documented, and test seams and process-wide switches not exported.

**Why.** `Firmware.Build` was exported beside an unexported string it had to agree with, so a hand-built value passed a check yet printed empty (#167). `Operation.Name` was a string, so a misspelling was a different operation (#111). Adding a method to `Reader` forced every implementer to change (0.8.0). A validation helper that exists for tests was public. Renames were needed in 0.2.0, 0.3.0, 0.4.0 and 0.8.0, each of which a look at the surface with this question would have avoided.

**Design question.** Could I add a field, value, method or argument next year without a break? What if a user builds the value by hand, compares it, switches over it, implements it or embeds it? What is its zero value, and what does a changed default do to an existing caller? `model.Reader` is the one wide interface on purpose, and it agrees with the rule against an interface that implementers must grow: the rule that a run-time type assertion that decides behavior means the interface lacks a method says to hand a gate the wide `Reader` (it must look at several settings) and not one method plus an assertion for the rest. `Reader` has one real implementer, `*Client`; callers take it (a gate) and a test double embeds it, so a method added to it breaks nobody who embeds, but it does break a type that implements all of it by hand (0.8.0 added `SwitchMAC`). So a method added to `Reader` is an exported change like any other: route it here, ask whether a gate needs it, and give it a migration row that says to embed `Reader`.

**Test requirement.** `make apidiff` is clean or the break is listed with a migration row; tests with hand-built values (zero value, an unkeyed literal, a round trip through JSON and text); a runnable example for each new entry point.

**Automation.** `make:apidiff`, `lint:exhaustive`, `lint:revive` (argument limit), `lint:recvcheck`, `test:TestEveryRefusedArgumentIsErrInvalid`; `proposed:apishape(#195)` (an analyzer over the exported API: an exported field and an unexported field of the same name, a function with more than three parameters of one type, an exported interface above a size, a closed-set type without `Validate` or `String`).

**Adversarial probe.** Write the ten-line consumer in five hostile styles: an unkeyed literal, a copy compared with `==`, a `switch` over every enum value without a default, an implementation of every exported interface, a JSON round trip of every type.

<a id="dc-l09"></a>
### DC-L09 An emptiness check that is itself a timed read

**Rule.** A timeout counts as proof that nothing is there only if the read was seen to wait for it; a read that returns a timeout without having waited (its deadline passed before it started) proves nothing, and the check reads again, within the bound the loop already has. The rule narrows the window (a hold-up after the check's own clock read and before the poller's check is still not seen); closing it takes a read that never consults a deadline or a second confirming read.

**Why.** `Client.discard` emptied the socket before each request by reading with a deadline of `drainWait` (1 ms) and taking a timeout for "empty". Go's poller checks the deadline before it tries the socket, so a goroutine that was not scheduled for longer than 1 ms between setting the deadline and reading got a timeout with a datagram queued; the datagram was then read as the answer to the request just sent. It showed as a flaky stale-datagram test on a loaded macOS machine (#206), and as a window for any consumer on a loaded host (#207).

**Design question.** What does this check conclude from a timeout, and can the timeout come without the wait (the deadline already passed when the read began, a canceled or closed socket, a clock step)? What does the check do then, and what ends the loop (a bound, the context, `Close`)?

**Test requirement.** A test over the `datagramConn` seam with a clock the test controls (no sleeping): a timeout that did not wait, with data queued, does not end the check; a timeout that waited does; the exact minimum is a wait; a flood and a socket that only ever times out end at the bound; a closed socket and a canceled context end the check; a read error that is only ICMP noise does not end it, and ends at the bound when it never stops.

**Automation.** `test:TestADrainTimeoutThatDidNotWaitDoesNotProveTheSocketEmpty`, `test:TestAStaleReplyInTheSocketIsNotTheAnswerToTheNextRequest`, `test:TestAFloodOfStaleDatagramsDoesNotHoldARequestBack`; `reading:` a check that draws a conclusion from a timeout cannot be found by a script, and a deadline that is short compared with the scheduler's pauses is the signal.

**Adversarial probe.** Hold the reading goroutine for longer than the deadline between setting it and reading (a wrapper over the seam), with a datagram queued, a flood queued and nothing queued; assert what is left in the socket, the number of reads and that the check ends.
