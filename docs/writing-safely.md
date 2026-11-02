# Writing safely

The switch is real network equipment with no console: a wrong write can cut the management path or the uplink, and recovery is a physical factory reset. Every write therefore follows the same steps.

1. **Validate.** The arguments are checked and a bad value is refused before anything is sent: `errors.Is(err, escp.ErrInvalid)`, and the message names the value and what is allowed. The same checks are public (`Validate` on <!-- validators:begin (generated from escp.Settings: make settings-docs) -->`Config`, `Dot1QVLAN`, `Hostname`, `IPConfig`, `Impact`, `Kbps`, `LAG`, `LAGID`, `Mirror`, `PortBandwidth`, `PortConfig`, `PortID`, `PortPVID`, `PortPriority`, `PortVLAN`, `PortVLANID`, `Priority`, `QoSMode`, `Speed`, `StormConfig`, `VLANID`, `VLANMode`, `VLANModeSetting`, `VLANName`<!-- validators:end -->), so a front end can refuse a value before it reaches the client, with the very error the writer would give. A writer that names a port also reads the switch's port count once, on the first such write, to refuse ports the switch does not have: that is a read, and it happens before the gate.
2. **Ask the gate.** If `Config.WriteGate` is set it is consulted with the write's `Operation` (the method, the ports it names and its `Impact`). When it refuses, the method returns the gate's error unchanged and sends nothing.
3. **Write.** The write is sent with the credentials, which ride on every SET. If the switch answers with a non-zero code the error is a `*RejectedError` (it matches `ErrRejected`).

```go
c, err := escp.Dial(escp.Config{
	Host: host, Username: user, Password: escp.Password(pw),
	WriteGate: safety.Chain(
		safety.FirmwareGate(),                // only firmware builds this client was verified against
		safety.MaxImpact(escp.ImpactTraffic), // nothing that can cut the management path: no port setting, VLAN, LAG, address...
		safety.PortGate(),                    // only ports with no link and no LAG
	),
})
err = c.SetBandwidth(ctx, escp.PortBandwidth{Port: 3, Ingress: 10240})
var refused *safety.PortUseError
if errors.As(err, &refused) { /* the gate refused; nothing was sent */ }
```

A runnable version is `ExampleConfig_writeGate` in the package documentation.

## Gates

A gate is a plain function, `func(ctx, escp.Reader, escp.Operation) error`; write your own or combine with `safety.Chain`, which stops at the first refusal.

| Gate | Refuses |
|---|---|
| `safety.FirmwareGate()` | a write on a firmware build this library was not verified against (`*safety.FirmwareError`) |
| `safety.MaxImpact(i)` | a write whose class is above `i` (`*safety.ImpactError`) |
| `safety.PortGate()` | a write that touches a port with a link, in a link aggregation group, or that the switch does not have (`*safety.PortUseError`); for a write that replaces a group of ports it checks the group's current members too |
| `safety.NewDryRun().Gate()` | every write, with `safety.ErrDryRun`, after recording the operation; `Operations()` lists what would have been written |

`PortGate` reads through the client it guards, before every write it checks. A front end with an opt-in of its own (an "allow disruptive" flag) leaves it out and uses `safety.UseOf` and `safety.CheckPorts` to decide.

## Impact

`Impact` classes the kind of change a method is, by what it can do to the way the switch is reached. A method that can cut the management path or the uplink with the right arguments is `ImpactManagement`, whichever port it is aimed at; the class does not know which port is the uplink (that is what `PortGate` and your own knowledge are for).

- `ImpactCosmetic`: cannot affect traffic or the way the switch is reached.
- `ImpactTraffic`: rate limits, storm control, QoS and IGMP snooping: they slow or drop some traffic but take no port, address or VLAN away.
- `ImpactManagement`: an address or DHCP, the VLAN mode, VLAN membership and PVIDs, a port that is turned off or retuned, link aggregation, mirroring, loop prevention: anything that can cut the management path or the uplink with the right arguments.

The methods of each class (generated from `escp.Settings`, the one table the writers, this page and the README read):

<!-- impact-classes:begin (generated from escp.Settings: make settings-docs) -->

| Class | Methods |
|---|---|
| `ImpactCosmetic` | `SetHostname`, `SetLED` |
| `ImpactTraffic` | `SetBandwidth`, `SetStormControl`, `SetQoSMode`, `SetPortPriorities`, `SetIGMP` |
| `ImpactManagement` | `SetIPConfig`, `EnableDHCP`, `SetPort`, `SetLAG`, `SetPortMirror`, `SetLoopPrevention`, `SetVLANMode`, `SetPortBasedVLAN`, `SetDot1QVLAN`, `DeleteDot1QVLAN`, `SetPVIDs` |

<!-- impact-classes:end -->

**Choosing a ceiling.** `MaxImpact(ImpactTraffic)` is the strict gate: it refuses every method of the third class, so a front end that only tunes rates and QoS cannot cut anything by mistake. A front end that does write port settings or VLANs allows `ImpactManagement` and puts its protection elsewhere: `PortGate` (free ports only), its own confirmation, a dry run first.

## What the client does not do

- It does not know which ports carry traffic. Deciding whether a write is prudent (is the port in use? in a link aggregation group?) and verifying the result are the caller's job; `PortGate` and `safety.UseOf` answer the common question.
- Apart from `SetIPConfig`, which waits for the switch to answer at its new address, no write reads back to check that it took effect.
- A few writes read first, so they can send the rest unchanged or refuse a combination the switch would silently break: `SetHostname`, `SetIPConfig` and `EnableDHCP` (the discovery reply), `SetPortPriorities` (the QoS page), `SetBandwidth` and `SetStormControl` (the other one's page: an ingress limit and storm control exclude each other), and every write that names a port (the port count, once). These reads are not gated; their comments say so.
- **Restoring is explicit.** There is no "reset": to put a setting back, write the value you recorded before.

Deliberately **not** offered: user account and password, config restore, reboot, factory reset and firmware upgrade.
