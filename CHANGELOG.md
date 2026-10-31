# Changelog

All notable changes to the library. Versions before 1.0.0 may change the exported API in a minor
version; a breaking change is called out as such. The history before 0.1.0 is that of the package
`internal/escp` of [terraform-provider-tplinkeasysmart](https://github.com/davidlukac/terraform-provider-tplinkeasysmart),
whose commits this repository keeps.

## 0.2.0

- **Breaking changes**, made together before consumers settle on the 0.1 names; none changes behaviour, wire bytes or error texts. Rename with the table below.

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
