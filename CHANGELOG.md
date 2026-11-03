# Changelog

All notable changes to the library. Versions before 1.0.0 may change the exported API in a minor
version; a breaking change is called out as such. The history before 0.1.0 is that of the package
`internal/escp` of [terraform-provider-tplinkeasysmart](https://github.com/davidlukac/terraform-provider-tplinkeasysmart),
whose commits this repository keeps.

## 0.6.0

- **Behavior change** (no signature change; `make apidiff` reports one addition, `DialContext`):

  | 0.5.0 | 0.6.0 |
  |---|---|
  | `ParsePortID` documented plain decimal input but read it with `strconv.Atoi`, so `"+3"`, `"01"` and `"003"` were accepted as ports 3, 1 and 3 (#53) | only the canonical spelling is accepted: the text must be the number written in decimal without sign or leading zeros, so one port has one spelling. `"+3"`, `"01"`, `"003"`, `"-0"`, `"0x3"`, `"1_0"`, a non-ASCII digit and an overflowing number are refused with the existing "not a port number" error, which matches `ErrInvalid`. A caller that passed a signed or zero-padded port normalizes it first |

- **Added:** `DialContext(ctx, cfg)`. `Dial` looked a host name up without a context, so a slow resolver blocked the caller and canceling did nothing; `DialContext` resolves under `ctx` and returns its error (`errors.Is` matches `context.Canceled` and `context.DeadlineExceeded`). `Dial(cfg)` is `DialContext(context.Background(), cfg)`; a literal address and a MAC need no lookup (#54).
- **`escptest`:** `TLVType` is the fake's own type (`type TLVType int16`, documented) and no longer an alias of the internal wire codec's, so it has a page of its own on pkg.go.dev. `make apidiff` lists `TLVType`, `Setting.Type`, `Faults.OnGet` and `EnableRows` as incompatible because their type is no longer the internal one; no code outside the module can have held that type, and `escptest.TLVType(4352)`, `[]escptest.TLVType` and `Setting{Type: 4352}` compile as before, so no migration is needed (#55). `FreeUDPPort` no longer fails on Windows runners where the system forbids binding the TCP port that marks a reservation (the error "an attempt was made to access a socket in a way forbidden by its access permissions", seen in CI): such a port is handed out without the cross-process marker, and never twice in one process (#71).
- **Documentation:** `PROTOCOL.md` has one layout table per setting (`Type | Dir | Bytes | Layout | Status`) with the measurements and the evidence in notes below it; no layout or status changed (#56).
- **Internal and contributors** (no API change): the client keeps MAC addresses as `MAC` instead of `[6]byte` (#58) and its test seams in one unexported `seams` struct set through dial options (#57); the tests that waited for real timeouts wait less, about a third off the root package's run on the maintainer's machine (#59); tests were added for the mutants that survived a full mutation run, and the run now leaves one survivor, an equivalent one (efficacy 98.97% before, 99.83% after) (#60); `scripts/pkgdoc.sh` and `scripts/fuzz.sh` are Go programs and no inline Python is left in the repository (#61); every case of a table-style test runs as a subtest, and `make lint` fails on a loop over cases without `t.Run` (`internal/subtests`) (#73).
- **Verified on hardware:** the live tests (`go test -tags live`) passed on a TL-SG108E with firmware 20250710 on the candidate commit: 17 passed, none failed, the three that move the management address skipped; the switch was left as found.

## 0.5.0

- **Behavior changes** (no signature changes; `make apidiff` reports one addition, `MaxRetries`):

  | 0.4.0 | 0.5.0 |
  |---|---|
  | `Config.Retries` of any size validated (`Retries: 1000000` with the 3 s timeout holds a call for a month when a switch stays silent) | more than **`MaxRetries` (10)** is refused by `Config.Validate` and `Dial`, with an error that matches `ErrInvalid`. Zero still selects the default, a negative value still selects none |
  | a socket read that failed (other than a timeout) ended as `ErrNoReply` after all the retries | it is returned at once as itself, wrapped as `escp: receive: …` (`errors.Is` and `errors.As` reach the cause). ICMP notices on the socket (`ECONNREFUSED`, and `WSAECONNRESET` on Windows, where a UDP socket reports them: [golang/go#5834](https://github.com/golang/go/issues/5834)) are skipped, not treated as failures |
  | another device answering during the download of the configuration file ended as `ErrNoReply` | it is `ErrWrongSwitch`, like in every other request |
  | the module required Go 1.26.0 | it requires **Go 1.26.2**: 1.26.0 and 1.26.1 can corrupt the stack in a UDP read on Windows ([golang/go#78041](https://github.com/golang/go/issues/78041)), which crashed a CI job once |

- **Fixes in `escptest`:** the ports it hands out (`FreeUDPPort`, the fake's own) are also reserved across processes, with a TCP listener on the same port number that is held until the process exits. `go test` runs the test binaries of several packages at once, and two of them could be given the same "free" port (`bind: address already in use`). `escptest/switch.go` is split into files by concern (no change to the API), and the documentation says why its values are plain (`[6]byte`, `int`): a MAC is assignable to `escp.MAC` both ways, and a test keeps it so.
- **Documentation:** every reply captured from the two switches (discovery, key exchange, a read; both firmware builds) is addressed to 255.255.255.255 from port 29808, so `Discover` beside a `Client` is safe where broadcasts reach every socket that shares the port (observed on macOS, covered by a test on Linux, not verified on Windows; replies to writes were not captured). The guides, `PROTOCOL.md` and `SECURITY.md` say so.
- **Pipeline** (contributors, no API change): the release script is a Go program (`internal/release`, no Python), and the commit trailer comes from `RELEASE_COMMIT_TRAILER`; `actionlint` is pinned in `devtools` and runs in the pre-commit hook when workflow files change; the mutation runs bypass the test cache (a cached baseline made nearly every mutant "time out").

## 0.4.0

- **Breaking changes**, made together before 1.0, from the third review of the library. Two change what existing code does (the impact classes and the encoding of values), the rest are renames and signatures. None changes wire bytes.

  | 0.3.0 | 0.4.0 |
  |---|---|
  | `SetPort`, `SetPortMirror`, `SetPortBasedVLAN`, `SetDot1QVLAN` and `DeleteDot1QVLAN` declare `ImpactTraffic` | they declare **`ImpactManagement`**: a method that can cut the management path or the uplink with the right arguments (a port turned off or retuned, a mirror destination, VLAN membership) is Management whichever port it names, as the address, the VLAN mode, PVIDs, link aggregation and loop prevention already were. `ImpactTraffic` is now rate limits, storm control, QoS and IGMP; `ImpactCosmetic` is unchanged. **A front end that used `safety.MaxImpact(escp.ImpactTraffic)` to allow port settings or VLAN groups on free ports must raise the ceiling to `ImpactManagement` and keep `safety.PortGate`**; with the old ceiling those methods are refused with an `*ImpactError` |
  | `MAC` encoded as `[0,0,94,0,83,1]`, `PortSet` as the bitmap number, `Firmware` as `{"Build":"…"}` (the version string was lost), `QoSMode` and `Impact` as numbers | they encode as text with `encoding/json` and the YAML and TOML libraries, and read back: `"00:00:5e:00:53:01"`, `"2,3,5"` (`"none"` for the empty set), the version string as the switch reported it, `"port-based"`, `"traffic"`. Anything that stored the old forms must read them with the old library or convert them |
  | `RateStepKbps`, `MaxBandwidthKbps` (untyped) | `RateStep`, `MaxBandwidth`, both typed `Kbps` (comparing one with a bare `int64` needs a conversion) |
  | `Impact.Valid()`, `QoSMode.Valid()`, `Priority.Valid()` (bool) | `Validate() error` (matches `ErrInvalid`), like every other input type |
  | `VLANModeFrom(mtu MTUVLAN, portBased, dot1q bool)` | `VLANModeFrom(mtu MTUVLAN, portBased PortBasedVLAN, dot1q Dot1Q)` |
  | `PortBasedVLAN.GroupLimit(portCount int)`, `NewPortVLANLimit(groups, ports int)` | `PortBasedVLAN.GroupLimit(sys System)`; `NewPortVLANLimit` is removed |
  | `escptest`: `SystemSetBehaviour` | `SystemSetBehavior` (American English in exported names) |
  | `escptest`: `Switch.Port()` | `Switch.UDPPort()` (it is a UDP port; `PortSetting` takes a switch port) |
  | `escptest`: `ConfigFile()` | `BackupFile()` |
  | `escptest`: `Faults.GetFn` | `Faults.OnGet` |

- **Fixes:** `Config.Validate` and `Dial` refuse a `Host` that names no single switch: the unspecified address, a multicast group or the limited broadcast address (a write carries the password, so it would have gone to every device that listens), an IPv6 address, a `host:port`, a host with a space. `Kbps.Validate` and the session-key errors carry the `escp:` prefix the other errors have. A `BuildDate` that is empty (no build date) reads back from its text.
- **New:** `Settings()`, the one table of the web UI pages the client reads and writes with the class of each write (the writers read their `Impact` from it; the README table and the guides' lists are generated from it); `MarshalText` and `UnmarshalText` on `MAC`, `PortSet`, `Firmware`, `QoSMode` and `Impact`, and a validated `UnmarshalText` on `Speed` and `VLANMode` (a name no port or switch has is refused); `Speed.Validate` and `VLANMode.Validate` (the sets `ParseSpeed` and `ParseVLANMode` accept); `ParseImpact`.
- **Documentation:** American English throughout and a linter and a test that keep it; the runnable snippets of the README and the getting-started guide; the log attribute table lists all 27 attributes the code logs (a test fails otherwise); `docs/errors.md` and the lists of validators are checked or generated from the code; the guide says what is known about running `Discover` beside a client.
- **Quality gates and pipeline** (contributors, no API change):
  - **Linters and tests:** `forcetypeassert`, `dupword` and `errchkjson` joined the lint set, with a checked `udp.AddrPort` instead of the unchecked type assertions; `make lint` (so the pre-commit hook) now also lints for Linux and Windows, the other systems CI covers; the coverage badge command fails when it cannot write its output. New tests keep the text honest: American English in every text file (`TestTheTextIsAmericanEnglish`), nothing private to a maintainer's workspace (`TestNothingPrivateToAWorkspaceIsPublished`), every log attribute and every error in its guide, the generated tables current.
  - **Mutation testing:** the report directory is created (the CI job never wrote its report before), the programs are left out of the run (gremlins reports every mutant of a `package main` as surviving), `internal/mutationreport badge` prints a shields.io endpoint file, and the weekly full run publishes it as the "mutation score" badge of the README, next to a CodeQL badge.
  - **Weekly checks** (`weekly.yml`, `make soak`): the suite shuffled and repeated with the race detector on Linux, macOS and Windows, a vulnerability scan on its own schedule, and `TestUsedAndClosedClientsLeaveNoGoroutinesBehind`, a leak check that looks at what the goroutines run (`ESCP_SOAK=n` for more cycles).
  - One place that binds the reply port; `escptest` has no unchecked assertions.

## 0.3.0

- **Breaking changes**, made together before 1.0 so that consumers rename once (a consumer that does not use these names is not affected). None changes wire bytes. Rename with the table below.

  | 0.2.0 | 0.3.0 |
  |---|---|
  | `WriteGate` is `func(ctx, escp.InfoReader, Operation) error`; `InfoReader` | `func(ctx, escp.Reader, Operation) error`; `escp.Reader` has every reader of the switch (`Info`, `System`, `LED`, `LoopPrevention`, `Ports`, `LAGs`, `PortMirror`, `Bandwidth`, `StormControl`, `QoS`, `IGMP`, `VLANMode`, `MTUVLAN`, `PortBasedVLAN`, `Dot1Q`), and `*Client` implements it. A gate no longer type-asserts at run time; a test double embeds `escp.Reader` |
  | `Operation{Replaces: escp.GroupLAG, ID: 3}`; `GroupKind`, `GroupLAG`, `GroupPortVLAN`, `GroupDot1QVLAN`, `Operation.ID` | `Operation{Replaces: escp.Group{LAG: 3}}`; `Group` has `LAG LAGID`, `PortVLAN PortVLANID`, `Dot1QVLAN VLANID` and `IsZero()`; read `op.Replaces.LAG` and so on. `safety.PortGate` treats a group that names more than one kind as unreadable and refuses |
  | `ErrInvalidConfig` | `ErrInvalidConfigFile` (it is about a configuration *file*; `ErrInvalid` is the general refusal) |
  | `Dot1Q.MaxVLAN`, `PortBasedVLAN.MaxVLAN` | `Dot1Q.MaxVLANs`, `PortBasedVLAN.MaxGroups` |
  | `PortBandwidth.IngressKbps`, `EgressKbps` | `PortBandwidth.Ingress`, `Egress` (still `Kbps`) |
  | `PortStorm.RateKbps`, `StormConfig.RateKbps` | `Rate` |
  | `Mirror.MirrorPort` | `Mirror.Destination` |
  | `Client.PortStatistics` | `Client.PortStats` |
  | `SetPVIDs(ctx, map[PortID]VLANID)` | `SetPVIDs(ctx, []PortPVID)` |
  | `SetPortPriorities(ctx, map[PortID]Priority)` | `SetPortPriorities(ctx, []PortPriority)` |

  `SetPVIDs` and `SetPortPriorities` now take what their readers return, like `SetLAG` and `SetPortBasedVLAN`; a port listed twice is refused with `ErrInvalid`, and the first fault is reported in the order given (it was map order, so a request with several faults named a different one on each run).

- **Fixes:** `Dial` refuses a `Config` it cannot use (`Config{}`, a negative `Timeout`, a UDP port outside 1 to 65535) with an error that matches `ErrInvalid`, instead of failing on the first call; the check is also available as `Config.Validate()`. A read the switch refuses, and a refused session key, are now a `*RejectedError` that matches `ErrRejected`, as `docs/errors.md` promised (only writes were). A `Password` no longer appears in clear text when a struct holding it is encoded (`json.Marshal`): it encodes as `[redacted]`; decoding is unchanged, and `ConfigFile` is deliberately not redacted (encode `Base64()`).
- **New:** a `Validate` method for every value a writer takes (`PortConfig`, `StormConfig`, `PortBandwidth`, `Mirror`, `Dot1QVLAN`, `PortPVID`, `PortPriority`), called by the writers, so a front end refuses at plan time exactly what the writer would refuse, with the same error; `ParseVLANMode`.
- **Visible text:** `safety.ErrDryRun` reads `safety: dry run: ...`; in `SetPort`, when both the port and the speed are wrong, the port is reported first. Matching by `errors.Is` is unchanged.
- **Documentation and policy:** the doc comments, guides and `PROTOCOL.md` no longer refer to anything private to the maintainer's workspace; a code of conduct (Contributor Covenant 2.1); the versioning convention for 0.x: a break or an addition is a minor version, a fix a patch.
- **Quality gates** (for contributors, no API change): a stricter linter in CI and the pre-commit hook (complexity limits, `unparam`, `gocritic`, `nolintlint`, `exhaustive` and others) and the long functions it found split; mutation testing with gremlins (`make mutation`, `make mutation-diff`, a weekly full run that fails below 90% efficacy, informational on pull requests); tests that pin what the mutation runs found; the coverage policy lets only a process entry point (`func main`) be excluded (30 excluded statements became 3); `escptest`'s packet handler is split into one method per operation.

## 0.2.0

- **Breaking changes**, made together before consumers settle on the 0.1 names; none changes behaviour or wire bytes. The one visible text that changes is the name of an 802.1Q write as a write gate sees it: `Operation.Name`, and so `*safety.ImpactError` messages, the `operation` log attribute and the dry-run list, is now `SetDot1QVLAN` / `DeleteDot1QVLAN` (was `SetVLAN8021Q` / `DeleteVLAN8021Q`), following the method names. Rename with the table below.

  | 0.1.0 | 0.2.0 |
  |---|---|
  | `VLAN8021Q`, `SetVLAN8021Q`, `DeleteVLAN8021Q` | `Dot1QVLAN`, `SetDot1QVLAN`, `DeleteDot1QVLAN` |
  | `ValidateIPConfig(cfg)`, `ValidateLAG(id, ports)`, `ValidateVLANMode(s)` | `cfg.Validate()`, `LAG{ID: id, Ports: ports}.Validate()`, `s.Validate()` |
  | `SetLAG(ctx, id, ports)`, `SetPortBasedVLAN(ctx, id, ports)` | `SetLAG(ctx, LAG{ID: id, Ports: ports})`, `SetPortBasedVLAN(ctx, PortVLAN{ID: id, Ports: ports})`; `PortVLAN` gets a `Validate` method |
  | `safety.Row`, `safety.Compatibility()` | `safety.CompatibilityRow`, `safety.CompatibilityRows()` |
  | `escptest`: `sw.Configure(func(f *escptest.Switch) { f.DropSets = true })` | `sw.Configure(func(f *escptest.Faults) { f.DropSets = true })`: the fault fields (`GetFn`, `RSADrop`, `RSACode`, `DropSets`, `DiscoverReply`, `RejectSetsFrom`, `RejectCode`, `StaleAfterSet`, `OnSystemSet`, `AfterSystemSet`) are fields of `Faults`, and a `Switch` has no exported fields |
  | `escptest.Loopback`, `escptest.DefaultMAC`, `escptest.DefaultLease` (variables) | `escptest.Loopback()`, `escptest.DefaultMAC()`, `escptest.DefaultLease()` (functions) |
  | `sw.SetsDropped()`, `sw.Port7Setting()`, `sw.State()` | `sw.DroppedSets()`, `sw.PortSetting(port)`, `sw.LED()` and `sw.WriteCount()` |

- **New:** `escptest.Listen`, `Switch.Close` and `FreePort` for code that has no `testing.TB`, such as an example; runnable examples with checked output for `Client.Info`, `Client.Ports`, a write behind a gate, `NewPortSet`, the `safety` gates and `escptest`.
- **Documentation:** the package overview is short and user-facing; the guides are in `docs/` (getting started, writing safely, errors, logging, testing, architecture); the README is the front page. `make pkgdoc` renders the package pages as pkg.go.dev shows them.
- **Internal:** the coverage tool moved to `internal/coverpolicy` (it was listed as a public command); the release workflow runs when a tag is pushed and verifies that the version installs.

## 0.1.0

- **First release as a module of its own**, extracted with its history from `internal/escp` of [terraform-provider-tplinkeasysmart](https://github.com/davidlukac/terraform-provider-tplinkeasysmart) v0.32.1, the code behind the provider. The API may still change in any minor version until 1.0.
- **Client** (`escp`): `Dial` and `Config`; typed reads and writes for the device, the management address and DHCP lease, ports, link aggregation, port mirror, bandwidth and storm control, QoS, IGMP snooping, loop prevention, LED, port-based, MTU and 802.1Q VLANs and the VLAN mode, port statistics and the configuration backup; every value validated before anything is sent; `Discover` and `Locate` (find a switch by its MAC address) without credentials.
- **Safety** (`safety`): a `WriteGate` consulted before every write, with gates for firmware builds the client was verified against, an impact ceiling, ports in use and a dry run, plus the compatibility table (`compatibility.json`).
- **Testing** (`escptest`): a fake switch that speaks the protocol on a loopback UDP port, optionally stateful and enforcing the rules measured on hardware.
- **Logging**: a caller-supplied `*slog.Logger`; attribute names are a contract (README).
- **Verified on** TL-SG108E hardware v6, firmware 1.0.0 Build 20230218 and 20250710. Other models are untested.
- **Differences from the provider's copy:** Go 1.26 or later; the live tests read `ESCP_*` variables; `escptest` has `PortSetting(port)` and `LED()` instead of `Port7Setting()` and `State()`.
