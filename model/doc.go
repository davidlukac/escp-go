// Package model holds the values of the Easy Smart protocol and the rules about them: the readings
// of a switch (Info, Port, LAG, QoS, the VLAN tables), the settings a front end writes (PortConfig,
// Mirror, Dot1QVLAN, IPConfig), the small types they are made of (MAC, PortID, PortSet, Speed, Kbps,
// Firmware, Password), their limits, their validation, and what a write gate is told about a write
// (Operation, Impact, Method, Reader, WriteGate) together with the table of what the library reads and
// writes (Settings).
//
// It has no I/O, no sockets and no state: a value is a plain Go value, comparable where it can be, and
// every input type has a Validate method that refuses exactly what the client refuses (the error
// matches ErrInvalid). That is what lets a front end check a value early, a test double speak the
// same types as the client (package escptest), and a policy (package safety) depend on the model
// without depending on the client. The client that sends these values, Dial and its Client, is in
// package escp, which returns them.
//
// The names a program uses most are short: model.PortID, model.MAC, model.PortConfig. Package escp
// documents how to connect and what each call does; this package documents the values.
//
// Like the rest of the module it is version 0: names may change in any minor version, and every
// change is listed in CHANGELOG.md.
package model
