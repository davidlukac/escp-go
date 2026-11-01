// Package escp is a client for the TP-Link Easy Smart UDP management protocol (ESCP), as spoken
// by the TL-SG108E v6 switches. It reads and writes the switch's settings, finds switches on the
// network and validates every value before anything is sent.
//
// The switch is real network equipment with no console: a wrong write can cut the management path
// or the uplink, and recovery is a physical factory reset. The protocol has no real secrecy either
// (see Security). Read the guide "Writing safely" before the first write.
//
// # Using a client
//
// Dial prepares a Client for one switch; nothing is sent until the first call. Reads (Ports, QoS,
// Dot1Q and the rest, named for what they return) cannot change the switch. Writes are the methods
// named Set... and Delete...
//
//	c, err := escp.Dial(escp.Config{Host: "192.0.2.11", Username: "admin", Password: escp.Password(pw)})
//	if err != nil { ... }
//	defer c.Close()
//	ports, err := c.Ports(ctx)
//
// Use one Client per switch and share it between goroutines: it sends one request at a time,
// because the switch answers one at a time. Clients for different switches work side by side. Two
// Clients for the same switch do not work, because the switch keeps a single session.
//
// Discover finds the switches on the network without credentials or addresses; Locate finds one
// by its MAC address. Config.MAC pins a client to a switch, so that a request to an address that
// now belongs to another device fails with ErrWrongSwitch before anything is written to it, and
// finds the switch again when its address changed.
//
// # Writing safely
//
// Every write validates its arguments (a bad value is refused with ErrInvalid before anything is
// sent), asks the optional Config.WriteGate with the write's Operation and Impact, and only then
// sends it. The safety package provides gates for firmware builds the client was not verified
// against, for a ceiling on the impact, for ports in use and for a dry run, and Chain combines
// them. The client does not know which ports carry traffic and, apart from SetIPConfig, does not
// read back after a write: deciding whether a write is prudent and checking the result are the
// caller's job. There is no reset; to restore a setting, write the value you recorded.
//
// # Errors
//
// Test errors with errors.Is and errors.As, never by message: ErrInvalid (refused before any
// write), ErrNoReply, ErrRejected and *RejectedError (the switch refused a write), ErrWrongSwitch,
// ErrNotFound, ErrMoveUnconfirmed, ErrNoInterface, ErrInvalidConfigFile and ErrReplyTooLarge. The
// gates of package safety return their own typed errors.
//
// # Logging
//
// The library writes nowhere on its own. Pass a *slog.Logger in Config.Logger or
// DiscoverOptions.Logger and it reports through that; nil discards everything. Every line
// carries component=escp, attribute names are a stable contract, and no message carries a
// password, a user name or a session key.
//
// # Guides
//
// Longer guides are in the repository, https://github.com/davidlukac/escp-go/tree/main/docs:
// getting started, writing safely, errors, logging, testing against the fake switch (package
// escptest) and the architecture. The wire protocol is PROTOCOL.md at the root.
//
// # Stability
//
// The module is version 0: until a 1.0 release, exported names, types and error values may change
// in any minor version, and every change is listed in CHANGELOG.md. Where something outside the
// code depends on a spelling (the names of Speed and QoSMode values are kept in saved state by
// the Terraform provider), the doc comment of that name says so.
//
// # Security
//
// ESCP has no real secrecy. The password travels in every write, protected only by RC4 with a
// key exchanged under a 64-bit RSA key, which anyone who can capture the traffic can break. Use it
// on a network you trust, and never log a Config.
package escp
