# escp: a Go client for TP-Link Easy Smart switches

<p>
  <a href="https://github.com/davidlukac/escp-go/actions/workflows/test.yml"><img alt="Tests" src="https://github.com/davidlukac/escp-go/actions/workflows/test.yml/badge.svg?branch=main"></a>
  <a href="https://pkg.go.dev/github.com/davidlukac/escp-go"><img alt="Go Reference" src="https://pkg.go.dev/badge/github.com/davidlukac/escp-go.svg"></a>
  <a href="https://github.com/davidlukac/escp-go/tags"><img alt="Version" src="https://img.shields.io/github/v/tag/davidlukac/escp-go?sort=semver&label=version&color=7c3aed"></a>
  <a href="go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/davidlukac/escp-go?logo=go&logoColor=white&color=00ADD8"></a>
  <a href="CONTRIBUTING.md#2-tests"><img alt="Test coverage" src="https://img.shields.io/endpoint?url=https%3A%2F%2Fraw.githubusercontent.com%2Fdavidlukac%2Fescp-go%2Fbadges%2Fcoverage.json"></a>
  <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-blue"></a>
</p>

`escp` talks the switches' native UDP management protocol (ESCP), as spoken by the vendor's configuration utility. It reads and writes the settings of a TL-SG108E v6, finds switches on the network, validates every value before anything is sent, and ships the safety gates and a fake switch that a front end needs to use it responsibly. It is the library behind the [Terraform provider](https://github.com/davidlukac/terraform-provider-tplinkeasysmart) for these switches, and is written to be reused by a command line client and by other programs.

**Status: version 0.** The API may change in any minor version until 1.0; [CHANGELOG.md](CHANGELOG.md) lists every change. Which hardware and firmware it was verified on is in the [compatibility table](#compatibility); the wire format is in [PROTOCOL.md](PROTOCOL.md).

> **The switch is real network equipment with no console.** A wrong write can cut the management path or the uplink, and recovery is a physical factory reset. Read [Writing safely](docs/writing-safely.md) before the first write.

## Install

```sh
go get github.com/davidlukac/escp-go
```

Go 1.26 or later. The module has one dependency, `golang.org/x/sys` (to share the reply port). It is tested on Linux, macOS and Windows against a fake switch; FreeBSD builds. Pin an exact version while the module is 0.x.

## Quick start

```go
c, err := escp.Dial(escp.Config{Host: "192.0.2.11", Username: "admin", Password: escp.Password(pw)})
if err != nil { /* ... */ }
defer c.Close()

info, err := c.Info(ctx)   // model, firmware, MAC, address: no credentials needed
ports, err := c.Ports(ctx) // reads cannot change the switch
for _, p := range ports {
	fmt.Printf("port %d: %s (configured %s)\n", p.ID, p.ActualSpeed, p.ConfiguredSpeed)
}
```

A write goes through the same client; give it a gate so that it cannot touch what you did not intend:

```go
c, err := escp.Dial(escp.Config{
	Host: host, Username: user, Password: escp.Password(pw),
	WriteGate: safety.Chain(safety.FirmwareGate(), safety.MaxImpact(escp.ImpactTraffic), safety.PortGate()),
})
err = c.SetPort(ctx, escp.PortConfig{ID: 3, Enabled: true, Speed: escp.SpeedAuto})
```

The examples in the [package documentation](https://pkg.go.dev/github.com/davidlukac/escp-go) run against the fake switch and show their output; [`examples/discover`](examples/discover/main.go) is a small read-only program for a real network.

## Guides

| | |
|---|---|
| [Getting started](docs/getting-started.md) | connect, find switches, find a switch by its MAC address |
| [Writing safely](docs/writing-safely.md) | what a write does; gates, impact classes, dry run |
| [Errors](docs/errors.md) | telling the failures apart |
| [Logging](docs/logging.md) | routing the library's log lines into yours |
| [Testing](docs/testing.md) | the fake switch, `escptest` |
| [Architecture](docs/architecture.md) | the code layout and conventions, for contributors |

## Packages

| Package | Import path | What it is |
|---|---|---|
| `escp` | `github.com/davidlukac/escp-go` | the client, the domain types, validators, `Discover` |
| `safety` | `github.com/davidlukac/escp-go/safety` | write gates (firmware, ports, impact, dry run) and the compatibility table |
| `escptest` | `github.com/davidlukac/escp-go/escptest` | a fake switch on a loopback UDP port, for tests |

## What it can read and write

| Setting (web UI page) | Read | Write | Types | Impact of the write |
|---|---|---|---|---|
| Device (System Info) | `Info`, `System` | `SetHostname`, `SetIPConfig` | `Info`, `System`, `IPConfig` | cosmetic, management |
| LED | `LED` | `SetLED` | `bool` | cosmetic |
| Ports (Port Setting) | `Ports` | `SetPort` | `Port`, `PortConfig`, `Speed` | traffic |
| Port statistics | `PortStatistics` | (read only) | `PortStats` | |
| Link aggregation (Port Trunk) | `LAGs` | `SetLAG` | `LAG` | management |
| Port mirror | `PortMirror` | `SetPortMirror` | `Mirror` | traffic |
| Bandwidth control | `Bandwidth` | `SetBandwidth` | `PortBandwidth` | traffic |
| Storm control | `StormControl` | `SetStormControl` | `PortStorm`, `StormConfig` | traffic |
| QoS Basic | `QoS` | `SetQoSMode`, `SetPortPriorities` | `QoS`, `QoSMode`, `Priority` | traffic |
| IGMP snooping | `IGMP` | `SetIGMP` | `IGMP`, `IGMPConfig`, `IGMPGroup` | traffic |
| Loop prevention | `LoopPrevention` | `SetLoopPrevention` | `bool` | management |
| VLAN mode | `VLANMode` | `SetVLANMode` | `VLANMode`, `VLANModeSetting` | management |
| MTU VLAN | `MTUVLAN` | (through `SetVLANMode`) | `MTUVLAN` | |
| Port-based VLAN | `PortBasedVLAN` | `SetPortBasedVLAN` | `PortBasedVLAN`, `PortVLAN` | traffic |
| 802.1Q VLAN and PVID | `Dot1Q` | `SetDot1QVLAN`, `DeleteDot1QVLAN`, `SetPVIDs` | `Dot1Q`, `Dot1QVLAN`, `PortPVID` | traffic, management |
| Configuration file | `ConfigBackup` | (restore is not offered) | `ConfigFile` | |

Deliberately **not** offered: user account, config restore, reboot, factory reset and firmware upgrade.

## Data you get back

- **Ports are numbers from 1**: a `PortID` for one, a `PortSet` for a set (a comparable value held as the wire bitmap: no duplicates, ascending, `Union`/`Minus`/`Intersect`; `escp.NewPortSet(2, 3)` refuses a port outside 1 to `MaxPort`).
- **Closed sets are types** with `String`, `Parse...`, `Valid` and a list function: `Speed` (`ConfigurableSpeeds`), `QoSMode` (`QoSModes`), `Priority` (1 to 4, as the web UI shows it), `VLANMode` (`VLANModes`). The spellings are the web UI's and the Terraform provider's; consumers keep them in saved state, so they do not change.
- **Readers return values, not pointers.** Counters in `PortStats` are the switch's 32-bit counters and wrap.
- **Limits and validators are public** so a front end can refuse a bad value before it reaches the client: every input type has a `Validate` method (`PortID`, `VLANID`, `Kbps`, `VLANName`, `Hostname`, `LAG`, `PortVLAN`, `IPConfig`, `VLANModeSetting`), and the constants they use are exported (`MaxPort`, `RateStepKbps`, `MaxBandwidthKbps`, `LAGGroupCount`, ...).

## Compatibility

<!-- compatibility-table:begin (generated from safety/compatibility.json: make compat-table) -->

| Switch | Hardware | Firmware | Status |
|---|---|---|---|
| TL-SG108E | v6 | 1.0.0 Build 20230218 | **Verified**: reads and writes checked on a real device, and the settings read back. The VLAN mode, 802.1Q VLAN and PVID writes were not tried on this build (they are verified on 20250710; same protocol generation). |
| TL-SG108E | v6 | 1.0.0 Build 20250710 | **Verified**: reads; the writes on unused ports (including mirror egress, unknown-unicast storm control and the VLAN mode, 802.1Q VLAN and PVID), the management address and the DHCP lease (the switch found again by its MAC address). LAG creation works as on 20230218 (group 1 takes ports 1-4, group 2 ports 5-8). |
| TL-SG108E | v6 | builds newer than 20250710 | Untried; reads work; `safety.FirmwareGate` refuses writes unless the caller opts in. Start with the reads |
| TL-SG108E | other hardware versions | any | Untested |
| Other Easy Smart models (TL-SG105E, TL-SG1016DE, ...) | any | any | Untested; reports wanted |
| Any | any | firmware **before the 2023 RSA key exchange** | Not supported: the client always performs the key exchange and has never been tried against the older login |

<!-- compatibility-table:end -->

## Security

ESCP has no real secrecy: the password travels in every write, protected only by RC4 with a key exchanged under a 64-bit RSA key that anyone who can capture the traffic can break. Use it on a network you trust, never log a `Config`, and do not reuse the switch password elsewhere.

## Disclaimer

This is an independent project. It is not affiliated with, endorsed by or supported by TP-Link; the protocol was worked out from the vendor's configuration utility, earlier open-source clients and observation of the switches. TP-Link, Easy Smart and the model names are trademarks of their owners. Use it at your own risk (see the license, and "Writing safely").

## See also

[Documentation index](docs/README.md) · [PROTOCOL.md](PROTOCOL.md) (wire format and measured behaviour) · [CONTRIBUTING.md](CONTRIBUTING.md) (how to add or change a setting) · [RELEASING.md](RELEASING.md) · [SECURITY.md](SECURITY.md)
