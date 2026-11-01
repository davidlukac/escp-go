# Testing against the fake switch

`escptest` is an in-process TL-SG108E that speaks ESCP over loopback UDP, for tests of code that uses the client. It is the same fake the library's own tests use. It is a test double, no substitute for a real switch, and must never be used outside tests.

```go
func TestMyCode(t *testing.T) {
	sw := escptest.Start(t) // stopped when the test ends; escptest.Listen() when there is no testing.T
	sw.EnableModel(map[int]escptest.LinkSpeed{1: escptest.Link1000Full}) // port 1 has a link

	c, err := escp.Dial(escp.Config{
		Host: "127.0.0.1", SwitchPort: sw.Port(), HostPort: escptest.FreeUDPPort(t),
		BroadcastAddr: escptest.Loopback(),
		Username: escptest.Username, Password: escptest.Password,
	})
	// ... use c, then ask the fake what it saw: sw.WriteCount(), sw.AllSets(), sw.LastSet()
}
```

`Config.SwitchPort`, `HostPort` and `BroadcastAddr` exist only for this; a real switch always uses UDP ports 29808 and 29809. The package documentation has runnable examples.

## What it does

By default it is a scripted switch: it answers the key exchange, checks the credentials on every SET, sends the configuration backup in fragments, and reports firmware `escptest.DefaultFirmware` (`SetFirmware` changes it). Three switches make it remember and enforce:

| Call | Effect |
|---|---|
| `EnableState` | every accepted SET is stored and answered by later GETs |
| `EnableRows` | tables with one element per row (ports, priorities, rates) |
| `EnableModel` | a model of a freshly reset switch that enforces the rules between settings (refusals, exclusivity, derived values) as measured on hardware, firmware 20250710, and 20230218 where a comment says so; it implies the first two |

The naming is one convention: `Enable...` switches a behavior on, `Set...` seeds a value (`SetMAC`, `SetFirmware`, `SetLease`), and the accessors read back what the switch saw (`WriteCount`, `LastSet`, `AllSets`, `PortSetting`, `LED`, `Hostname`, `Received`).

## Failures to survive

Faults are set inside `Configure`, which holds the switch's lock:

```go
sw.Configure(func(f *escptest.Faults) {
	f.RejectSetsFrom = 2 // reject the second authenticated SET and every later one
})
```

`Faults` offers silence, a rejected key exchange, ignored or rejected writes, a scripted GET reply, a replaced or stale discovery reply, and what the switch does after a change of its address. `DropPacket` and `DropFirst` lose requests, `Mute` silences the switch, and `SetBackupMode` and `SetBackupScript` make the configuration backup misbehave.

A test that checks "nothing was written" should assert `sw.WriteCount() == 0`, and for a write that was lost on purpose also `sw.DroppedSets()`, so it does not pass for the wrong reason.

## What it is not

It keeps no state until you enable it, models only the TL-SG108E, and answers faster and more reliably than a real management CPU. A rule that is only assumed is marked as such in a comment in the source. Verify a change on a real switch (the `live` tests in the repository) before you trust it.
