# Writing safely

The switch is real network equipment with no console: a wrong write can cut the management path or the uplink, and recovery is a physical factory reset. Every write therefore follows the same steps.

1. **Validate.** The arguments are checked and a bad value is refused before anything is sent: `errors.Is(err, escp.ErrInvalid)`, and the message names the value and what is allowed. The same checks are public (`Validate` on `Config`, `PortID`, `VLANID`, `Kbps`, `VLANName`, `Hostname`, `PortConfig`, `StormConfig`, `PortBandwidth`, `Mirror`, `LAG`, `PortVLAN`, `Dot1QVLAN`, `IPConfig` and `VLANModeSetting`), so a front end can refuse a value before it reaches the client, with the very error the writer would give. A writer that names a port also reads the switch's port count once, on the first such write, to refuse ports the switch does not have: that is a read, and it happens before the gate.
2. **Ask the gate.** If `Config.WriteGate` is set it is consulted with the write's `Operation` (the method, the ports it names and its `Impact`). When it refuses, the method returns the gate's error unchanged and sends nothing.
3. **Write.** The write is sent with the credentials, which ride on every SET. If the switch answers with a non-zero code the error is a `*RejectedError` (it matches `ErrRejected`).

```go
c, err := escp.Dial(escp.Config{
	Host: host, Username: user, Password: escp.Password(pw),
	WriteGate: safety.Chain(
		safety.FirmwareGate(),                // only firmware builds this client was verified against
		safety.MaxImpact(escp.ImpactTraffic), // nothing that can cut the management path
		safety.PortGate(),                    // only ports with no link and no LAG
	),
})
err = c.SetPort(ctx, escp.PortConfig{ID: 3, Enabled: true, Speed: escp.SpeedAuto})
var refused *safety.PortUseError
if errors.As(err, &refused) { /* the gate refused; nothing was sent */ }
```

A runnable version is `ExampleConfig_writeGate` in the package documentation.

## Gates

A gate is a plain function, `func(ctx, escp.Reader, escp.Operation) error`; write your own or combine with `safety.Chain`, which stops at the first refusal.

| Gate | Refuses |
|---|---|
| `safety.FirmwareGate()` | a write on a firmware build this library was not verified against (`*safety.FirmwareError`) |
| `safety.MaxImpact(i)` | a write whose worst case is above `i` (`*safety.ImpactError`) |
| `safety.PortGate()` | a write that touches a port with a link, in a link aggregation group, or that the switch does not have (`*safety.PortUseError`); for a write that replaces a group of ports it checks the group's current members too |
| `safety.NewDryRun().Gate()` | every write, with `safety.ErrDryRun`, after recording the operation; `Operations()` lists what would have been written |

`PortGate` reads through the client it guards, before every write it checks. A front end with an opt-in of its own (an "allow disruptive" flag) leaves it out and uses `safety.UseOf` and `safety.CheckPorts` to decide.

## Impact

`Impact` is the worst a method can do with any arguments:

- `ImpactCosmetic`: the LED, the device description.
- `ImpactTraffic`: port settings, rate limits, storm control, QoS, IGMP, mirroring, VLAN groups.
- `ImpactManagement`: the IP settings, the VLAN mode, PVIDs, link aggregation, loop prevention.

## What the client does not do

- It does not know which ports carry traffic. Deciding whether a write is prudent (is the port in use? in a link aggregation group?) and verifying the result are the caller's job; `PortGate` and `safety.UseOf` answer the common question.
- Apart from `SetIPConfig`, which waits for the switch to answer at its new address, no write reads back to check that it took effect.
- A few writes read the current settings first so they can send the rest unchanged (`SetHostname`, `SetPortPriorities`); their comments say so.
- **Restoring is explicit.** There is no "reset": to put a setting back, write the value you recorded before.

Deliberately **not** offered: user account and password, config restore, reboot, factory reset and firmware upgrade.
