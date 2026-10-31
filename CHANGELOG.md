# Changelog

All notable changes to the library. Versions before 1.0.0 may change the exported API in a minor
version; a breaking change is called out as such. The history before 0.1.0 is that of the package
`internal/escp` of [terraform-provider-tplinkeasysmart](https://github.com/davidlukac/terraform-provider-tplinkeasysmart),
whose commits this repository keeps.

## 0.1.0

- **First release as a module of its own**, extracted with its history from `internal/escp` of [terraform-provider-tplinkeasysmart](https://github.com/davidlukac/terraform-provider-tplinkeasysmart) v0.32.1, the code behind the provider. The API may still change in any minor version until 1.0.
- **Client** (`escp`): `Dial` and `Config`; typed reads and writes for the device, the management address and DHCP lease, ports, link aggregation, port mirror, bandwidth and storm control, QoS, IGMP snooping, loop prevention, LED, port-based, MTU and 802.1Q VLANs and the VLAN mode, port statistics and the configuration backup; every value validated before anything is sent; `Discover` and `Locate` (find a switch by its MAC address) without credentials.
- **Safety** (`safety`): a `WriteGate` consulted before every write, with gates for firmware builds the client was verified against, an impact ceiling, ports in use and a dry run, plus the compatibility table (`compatibility.json`).
- **Testing** (`escptest`): a fake switch that speaks the protocol on a loopback UDP port, optionally stateful and enforcing the rules measured on hardware.
- **Logging**: a caller-supplied `*slog.Logger`; attribute names are a contract (README).
- **Verified on** TL-SG108E hardware v6, firmware 1.0.0 Build 20230218 and 20250710. Other models are untested.
- **Differences from the provider's copy:** Go 1.26 or later; the live tests read `ESCP_*` variables; `escptest` has `PortSetting(port)` and `LED()` instead of `Port7Setting()` and `State()`.
