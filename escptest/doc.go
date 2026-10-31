// Package escptest is an in-process TL-SG108E that speaks ESCP over loopback UDP, for tests of code
// that uses the escp client. It is a test double: no substitute for a real switch, and never to be
// used outside tests.
//
// It imports only the wire codec, never the client, so the escp package's own tests can use it
// without an import cycle.
//
// # What it does
//
// By default it is a scripted switch: it answers the key exchange, checks the credentials on
// every SET (Username and Password), sends the configuration backup in fragments and reports
// firmware DefaultFirmware (SetFirmware changes it). Three switches make it remember and
// enforce:
//
//   - EnableState: every accepted SET is stored and answered by later GETs.
//   - EnableRows: tables with one element per row (ports, priorities, rates).
//   - EnableModel: the rules between settings (refusals, exclusivity, derived values) as measured
//     on hardware, firmware 20250710, and 20230218 where a comment says so; it implies the first two.
//
// The names follow one convention: Enable... switches a behaviour on, Set... seeds a value (SetMAC,
// SetFirmware, SetLease), and the accessors read back what the switch saw (WriteCount, LastSet,
// AllSets, PortSetting, LED, Hostname, Received).
//
// # Failures to survive
//
// Faults are set inside Configure, which holds the switch's lock: silence, a rejected key exchange,
// ignored or rejected writes, a stale or replaced discovery reply, and what the switch does after
// a change of its address. DropPacket and DropFirst lose requests, Mute silences the switch, and
// SetBackupMode and SetBackupScript make the configuration backup misbehave.
//
// # Using it
//
// A test starts one switch and aims a client at it (see the example of Start):
// Config.SwitchPort, Config.HostPort and Config.BroadcastAddr exist for this, because a real
// switch always uses UDP ports 29808 and 29809. Setting is the fake's own type for requests and
// replies, so a test outside the escp module needs nothing from the client's internals.
//
// # Stability
//
// Version 0, like the escp package: names may change in any minor version.
package escptest
