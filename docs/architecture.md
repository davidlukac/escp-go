# Architecture and conventions

For people who work on the library. Users want the [guides](README.md); the checklist for adding or changing a setting is [CONTRIBUTING.md](../CONTRIBUTING.md).

## Packages

| Package | What it is |
|---|---|
| `escp` (the repository root) | the client, the domain types, validators and `Discover`/`Locate` |
| `safety` | the policy: write gates (firmware, ports, impact, dry run) and the compatibility table. No logging; it reads a switch only through the small interfaces it is given and reports what it finds as typed errors and values, so each front end words the refusal in its own vocabulary |
| `escptest` | a fake switch on a loopback UDP port, for tests ([guide](testing.md)); imports only the wire codec, never the client, so the client's own tests can use it without an import cycle |
| `internal/wire` | the packet codec, the session crypto and every TLV type number, which no caller needs |
| `internal/udp` | sharing the reply port between sockets, per operating system |
| `internal/coverpolicy` | the coverage policy checker and the badge (a build tool, not part of the API) |
| `examples/discover` | a read-only example program |

The module path is the repository's, `github.com/davidlukac/escp-go`, so the import paths are that, `.../escp-go/safety` and `.../escp-go/escptest`; the packages themselves are named `escp`, `safety` and `escptest`, and the documentation calls them that.

Dependencies point one way: `safety` and `escptest` depend on the client or the codec, never the other way round.

## Layout of the root package

A setting is one file: its types, its reader, its writer, its parser and its validation live together, and its tests sit in the file of the same name with the suffix `_test` (`ports.go`, `ports_test.go`). The settings are leaves: they depend on the client and on the shared helpers, never on each other (except `Port`, the type most of them refer to).

- **Settings:** `ports`, `lag`, `qos`, `bandwidth`, `storm`, `igmp`, `mirror`, `loopprevention`, `led`, `system`, `stats`, `backup`, `vlan_mtu`, `vlan_port`, `vlan_mode`, `vlan_8021q` (the reads) and `vlan_8021q_write`. `system.go` is the device description, `ip.go` the static management address (`SetIPConfig` and its lost-reply handling) and `dhcp.go` the lease (`EnableDHCP`).
- **The client:** `client.go` (`Client`, `Dial`, `Close`, the socket), `config.go` (`Config`, `WriteGate`, `InfoReader`, `Info`), `info.go` (reading `Info`), `session.go` (the key exchange and its retry), `transport.go` (packets, encryption, sending, receiving, fragments, deadlines), `request.go` (the one place that sends a gated SET and a GET, and the port count), `seq.go` (request and discovery sequence numbers) and `errors.go`.
- **Shared:** `mac.go` (the one place a switch's hardware address is parsed, compared and printed), `operation.go` (the `Operation` a gate sees and the `Impact` classes), `limits.go` (the public limits), `portset.go` (`PortID` and `PortSet`: the port numbers and the sets of them, which are the wire bitmap), `discover.go` (finding switches without credentials) and `logging.go` (the logger seam, [logging](logging.md)).

Tests that cut across sources have names of their own: `cancel_test` (cancellation), `parsers_test`, `readers_test`, `identity_test` (identity and lookup by MAC address), `write_gate_test`, `dryrun_test`, `fake_switch_test` (the fake itself), `fuzz_test` and `helpers_test`. The `live_*_test.go` files are manual tests against a real switch (`-tags live`).

## Naming

- A field that holds a port number is named for what the record is. A record that *is* a port (`Port`, `PortConfig`, `PortStats`) calls it `ID`; a record that *refers to* a port (`PortBandwidth`, `PortStorm`, `StormConfig`, `PortPVID`, `PortPriority`) calls it `Port`; a port with a role in the record is named for the role (`MTUVLAN.UplinkPort`, `Mirror.MirrorPort`).
- Readers are named for what they return (`Ports`, `QoS`, `LED`, `Dot1Q`), writers `Set...` and `Delete...`.
- The 802.1Q family shares the prefix `Dot1Q`: `Dot1Q` (the table the switch reports), `Dot1QVLAN` (one VLAN), `SetDot1QVLAN`, `DeleteDot1QVLAN`, and `VLANModeDot1Q` for the mode.
- Every type a writer takes can be checked: it has a `Validate` method that returns an error matching `ErrInvalid`, and the writer calls that method, so the two cannot disagree (`validate_test.go` pins it).
- Closed sets are types with a list function and, where the names are text, a `Parse...` function (`Speed`, `QoSMode`, `VLANMode`; `Priority` is a number with `Valid`). Their spellings are the web UI's and the Terraform provider's; consumers keep them in saved state, so they do not change.
- Attribute names in log lines are a contract ([logging](logging.md)).

## Rules the code keeps

- Protocol numbers are named constants with a doc comment; layout bytes stay in the root package and `internal/wire`.
- Every writer validates its arguments, asks the write gate, writes through `Client.setTLVs` in `request.go` (the only place that sends a SET with settings), and returns typed errors.
- Coverage is 100% of reachable statements (`make cover`); `escptest` is exercised by the library's own tests and not measured.
- `make pkgdoc` renders the package documentation the way pkg.go.dev does, from the working tree.
