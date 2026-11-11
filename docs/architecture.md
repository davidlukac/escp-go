# Architecture and conventions

For people who work on the library. Users want the [guides](README.md); the checklist for adding or changing a setting is [CONTRIBUTING.md](../CONTRIBUTING.md).

## Packages

| Package | What it is |
|---|---|
| `escp` (the repository root) | the client: `Dial`, `Client`, the transport, the session, the parsers and encoders of each setting, `Discover`/`Locate` |
| `model` | the values and the rules about them: readings, settings, the small types they are made of, their validators, what a write gate is told (`Operation`, `Impact`, `Method`, `Reader`, `WriteGate`) and `Settings()`. No I/O; it imports only the wire lengths and the error class |
| `safety` | the policy, which depends on the model and not on the client: write gates (firmware, ports, impact, dry run) and the compatibility table. No logging; it reads a switch only through the small interfaces it is given and reports what it finds as typed errors and values, so each front end words the refusal in its own vocabulary |
| `escptest` | a fake switch on a loopback UDP port, for tests ([guide](testing.md)); speaks the model's types (`model.MAC`, `model.PortID`) and the wire codec, never the client, so the client's own tests can use it without an import cycle |
| `internal/invalid` | the one error class of refused arguments, shared by the root and the model so that both return errors that match `ErrInvalid` |
| `internal/wire` | the packet codec, the session crypto and every TLV type number, which no caller needs |
| `internal/udp` | sharing the reply port between sockets, per operating system |
| `internal/coverpolicy` | the coverage policy checker and the badge (a build tool, not part of the API) |
| `internal/mutationreport` | the job summary of a mutation run (a build tool) |
| `internal/release` | cutting a version: the CHANGELOG entry, the checks, the tag (a build tool, `scripts/release.sh`) |
| `internal/fuzzrun` | every fuzz target of the module for a number of seconds each (a build tool, `scripts/fuzz.sh`) |
| `devtools/subtests` | the check `make lint` runs for what golangci-lint cannot see: a table of cases in a test must run each case with `t.Run` (a `go/analysis` command in the devtools module; its one source is here, and the provider repository gets a verbatim copy) |
| `devtools/analyzers` | the other checks `make lint` runs, one `multichecker`: `readwrite` (a read followed by a write outside the modify guard) and `lockwait` (a blocking operation under a lock needs a `// lockwait: <reason>`); one directory, copied verbatim into the provider repository like `devtools/subtests` |
| `internal/pkgdoc` | the package documentation rendered the way pkg.go.dev does, as HTML and text (a build tool, `scripts/pkgdoc.sh`) |
| `examples/discover` | a read-only example program |
| `examples/consumer` | a small program that uses `escp`, `model`, `safety` and `escptest` the way an outside module does; the release workflow builds it in a fresh module against the tagged version |

The module path is the repository's, `github.com/davidlukac/escp-go`, so the import paths are that, `.../escp-go/model`, `.../escp-go/safety` and `.../escp-go/escptest`; the packages themselves are named `escp`, `model`, `safety` and `escptest`, and the documentation calls them that.

Dependencies point one way: `escp` (the client) depends on `model`; `safety` and `escptest` depend on `model`; `model` depends on nothing of the module but `internal/wire` (lengths) and `internal/invalid`. `model` never imports the client, and a test (`model/layers_test.go`) says so.

## Layout of the root package and of the model

A setting is one file in each package, with the same name: in `model` its types and their validation (`model/ports.go`), in the root its reader, its writer and its parser (`ports.go`). Their tests sit in the file of the same name with the suffix `_test`. The settings are mostly leaves: they depend on the client and on the shared helpers (`request.go`, `limits.go` and, in `model`, `model/operation.go`, `model/portset.go`), and otherwise only on `Port` (`ports.go`), the type most of them refer to. The exceptions are few and listed so that a new one is a decision: `vlan_mode` composes the three VLAN readings; `vlan_port` reads the port-based VLAN page and the port count (TLV 10) for the limit on group ids; `bandwidth` and `storm` share `limiter.go` (the switch's one limiter for incoming traffic); `igmp` and the PVID types use `VLANID`, which is declared in `model/vlan_8021q.go`; and the device cluster `info`, `system`, `ip` and `dhcp` refer to each other, being one subject (the identity and the management address of the switch).

- **Settings:** `ports`, `lag`, `qos`, `bandwidth`, `storm`, `igmp`, `mirror`, `loopprevention`, `led`, `system`, `stats`, `backup`, `vlan_mtu`, `vlan_port`, `vlan_mode`, `vlan_8021q` (the reads) and `vlan_8021q_write`. `system.go` is the device description, `ip.go` the static management address (`SetIPConfig` and its lost-reply handling) and `dhcp.go` the lease (`EnableDHCP`).
- **The client:** `client.go` (`Client`, `Dial`, `Close`, the socket), `config.go` (`Config`, `Config.Validate`; the `WriteGate` and `Reader` types are `model/config.go`), `info.go` (`Info`, the discovery probe of the switch the client talks to, and the lookup by MAC), `session.go` (the key exchange and its retry, and the drawing of a session key), `identity.go` (who the client believes the switch is: the pin from `Config.MAC` or the MAC of the first contact, and the device that answered in its place; it has a lock of its own so that reading it never waits for an exchange), `registry.go` (the claims of the clients of this process on their switches, `ErrSwitchInUse`), `settle.go` (the one polling loop behind the wait for a moved switch and the wait for a lease), `transport.go` (packets, encryption, sending, receiving, fragments, deadlines), `request.go` (the one place that sends a gated SET and a GET, and the port count), `seq.go` (request and discovery sequence numbers) and `errors.go`.
- **Shared:** `mac.go` (the length of a hardware address on the wire; `MAC` itself is `model/mac.go`), `limits.go` (reading a port mask out of a reply; the public limits are `model/limits.go`), `doc.go` (the package documentation), `discover.go` (finding switches without credentials), `limiter.go` (the switch's one limiter for incoming traffic, shared by the bandwidth and storm writers) and `logging.go` (the logger seam, [logging](logging.md)).
- **Only in `model`:** `model/firmware.go` (`Firmware`, `BuildDate`), `model/password.go` (`Password`, which never prints), `model/operation.go` (the `Operation` a gate sees and the `Impact` classes), `model/method.go` (`Method`, the one spelling of the names of the client's methods), `model/portset.go` (`PortID` and `PortSet`: the port numbers and the sets of them, which are the wire bitmap) and `model/settings.go` (`Settings()`, the one table of what the library reads and writes and with which impact).

Tests that cut across sources have names of their own: `cancel_test` (cancellation), `parsers_test`, `readers_test`, `identity_test` (identity and lookup by MAC address), `write_gate_test`, `dryrun_test`, `fake_switch_test` (the fake itself), `fuzz_test` and `helpers_test`. The `live_*_test.go` files are manual tests against a real switch (`-tags live`).

## Naming

- A field that holds a port number is named for what the record is. A record that *is* a port (`Port`, `PortConfig`, `PortStats`) calls it `ID`; a record that *refers to* a port (`PortBandwidth`, `PortStorm`, `StormConfig`, `PortPVID`, `PortPriority`) calls it `Port`; a port with a role in the record is named for the role (`MTUVLAN.UplinkPort`, `Mirror.Destination`).
- Readers are named for what they return (`Ports`, `QoS`, `LED`, `Dot1Q`), writers `Set...` and `Delete...`.
- The 802.1Q family shares the prefix `Dot1Q`: `Dot1Q` (the table the switch reports), `Dot1QVLAN` (one VLAN), `SetDot1QVLAN`, `DeleteDot1QVLAN`, and `VLANModeDot1Q` for the mode.
- Every type a writer takes can be checked: it has a `Validate` method that returns an error matching `ErrInvalid`, and the writer calls that method, so the two cannot disagree (`validate_test.go` pins it).
- Closed sets are types with a list function and, where the names are text, a `Parse...` function (`Speed`, `QoSMode`, `VLANMode`; `Priority` is a number, `Impact` a class). Every one has `Validate() error`, like every other input type: a method that answers a bare bool is not part of the API. Their spellings are the web UI's and the Terraform provider's; consumers keep them in saved state, so they do not change.
- Attribute names in log lines are a contract ([logging](logging.md)).

## Rules the code keeps

- Protocol numbers are named constants with a doc comment; layout bytes stay in the root package and `internal/wire`.
- Every writer validates its arguments, asks the write gate, writes through `Client.setTLVs` in `request.go` (the only place that sends a SET with settings), and returns typed errors.
- Coverage is 100% of reachable statements (`make cover`); `escptest` is exercised by the library's own tests and not measured. Mutation testing (`make mutation`, [CONTRIBUTING.md](../CONTRIBUTING.md)) checks that the tests would notice a wrong operator; the fake and the programs (the coverage tool, the report tool, the example) are left out of it.
- `make pkgdoc` renders the package documentation the way pkg.go.dev does, from the working tree.
- The classes of defect that reached a release, with the question to ask and the test to write for each, are in [defect-classes.md](defect-classes.md); most rules above are the mechanical side of one of them.
