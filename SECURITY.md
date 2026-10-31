# Security policy

## Reporting a vulnerability

Please report security issues **privately** through GitHub: *Security* tab > *Report a vulnerability* (private vulnerability reporting). Do not open a public issue for something exploitable. You can expect an acknowledgement within a week.

## Supported versions

The latest released version. The module is pre-1.0; fixes land on `main` and ship in the next release.

## Threat model, in one paragraph

The library speaks the switch's own management protocol (ESCP), which has **no real security on the wire**: the static RC4 key is public, the session-key exchange uses a 64-bit RSA modulus that is trivially breakable, there is no integrity protection, and a SET carries the admin password. This is a property of the device firmware, not something a client can fix. Anyone who can observe or inject UDP on the switch's layer-2 segment can recover credentials and forge replies. See [`PROTOCOL.md`](PROTOCOL.md).

## What that means for you

- Use it from a host on a **trusted management network** (a dedicated VLAN or an out-of-band segment), never across an untrusted LAN.
- Never log a `Config`; keep passwords out of source code and out of the logs you ship (the library's own log lines never carry a password, user name or session key).
- The client shares its UDP reply port (`SO_REUSEPORT`) so several switches can be managed at once. Other processes on the same host that bind the port the same way can see replies; do not run it on a shared multi-user host.
- The write gates (`safety`) protect against self-inflicted lockouts, not against an attacker on the segment.

## In scope

Bugs in this library that leak credentials (logs, error messages), bypass a write gate, panic or hang on malformed replies, or let a reply from the wrong device be accepted. Weaknesses of the vendor protocol itself are documented above and are out of scope.
