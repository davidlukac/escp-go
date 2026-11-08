# ESCP: the TP-Link Easy Smart UDP management protocol

This is the wire protocol the `escp` package speaks. It was worked out from the vendor's configuration utility, from earlier open-source clients ([smrt](https://github.com/rgl/smrt) and its forks) and by observing a TL-SG108E hardware v6 (firmware 1.0.0, builds 20230218 and 20250710). The firmware released in 2023 added the RSA key exchange described below.

The notes are descriptive, written for people who maintain this library or want to build a compatible client. Layouts marked "verified" were exercised against a real switch; the others come from the vendor utility.

How to read the layout tables below: **Dir** is `GET` (a read), `SET` (a write) or both; **Bytes** is the size of one TLV value; **Status** repeats what the notes say about a layout: *verified live* (with the firmware build where it says so), *vendor code* for a layout taken from the vendor utility, and *not exercised* for one nobody tried; a dash means the layout is not marked, which is the vendor utility's by the rule above. The measurements, dates and probe details are in the notes under each table.

## Transport

- UDP. Client binds `29809`, switch listens on `29808`. Same L2 segment only. Requests can be sent to the broadcast address **or** unicast to the switch IP; replies come back to port 29809 from the switch IP.
- Replies are **addressed to the limited broadcast address 255.255.255.255**, so every host of the segment receives them, not only the one that asked. This was captured with a second socket that shares the port and reads the destination address of each datagram: the three replies of a first read, in order a discovery (169 bytes), the key exchange (36 bytes) and the read (41 bytes), of both switches, firmware 20230218 and 20250710, from source port 29808 on the host's interface. Replies to writes were not captured.
- Header is 32 bytes, network byte order (`!bb6s6shihhhhi`): version(=1), op, switch MAC, host MAC, sequence (short), error code (int), total length (short, header + TLVs + end marker), fragment offset, flag, token id, checksum. Payload is TLVs `type(short) len(short) value`, terminated by `ff ff 00 00`. Strings are NUL-terminated, integers big-endian.
- **The switch MAC in the header of a reply is the switch's own, in every kind of reply.** Recorded on two TL-SG108E (firmware 20230218 and 20250710) with `TestLiveEveryReplyCarriesTheSwitchMAC`, which wraps the client's socket and decodes every datagram it receives during a new session: the discovery and read replies of 16 different reads (op 2), a read the switch refuses (op 2, error code -2), the reply to the session key exchange (op 4, which carries a non-zero code by design), the fragments of a config download (op 4) and, on the second switch only, the acknowledgment of a write of the value already set (op 4). 30 datagrams on the first switch and 32 on the second; none carried an all-zero switch MAC. A request carries zero there only when the sender does not know the MAC yet (a first discovery). The library therefore treats a reply with an all-zero switch MAC, or a discovery reply without the MAC description, from a client that has an identity (`Config.MAC`, or the MAC of the first contact) as coming from another device (`ErrWrongSwitch`). The MAC is not authentication: anyone on the segment can forge it; the check catches a misdirected reply and an address that changed hands.
- Op codes: 0 DISCOVERY, 1 GET, 2 reply to GET/DISCOVERY, 3 SET/LOGIN, 4 reply to SET. A reply echoes the request's sequence number; the client drops anything else.
- Every reply goes to UDP port 29809 whoever asked, so a reply can reach another client of the same host, and a late one can still be in a socket when the next request goes out. Two rules keep it from being taken for an answer: requests count through 0-999 and discoveries sent through a client (and Discover) draw from 1000-1999, and a client discards what is waiting in its socket before it sends. A regression run on hardware read a discovery reply as the answer to a LED, a mirror and a LAG read until this was done.

## Encryption (two RC4 variants)

1. **Static RC4 (256-byte state)**, used for discovery and the key-exchange packet. Its key is the one the vendor tooling ships with (public in several open-source clients); the state is re-initialized for every packet, the stream is not carried between packets.
2. **Session RC4 with an 8-byte state**: the same algorithm with state size 8, keyed by a random 8-character alphanumeric string generated per client run. Fresh state per packet.

Sending: discovery and the RSA key-exchange packet use static RC4; everything else uses the session RC4 once the exchange succeeded. Receiving: decrypt with static RC4; if the result looks like a header accept it, otherwise decrypt with the session RC4. A buffer looks like a header when it is at least 36 bytes (the 32-byte header and the end marker), byte 0 is 1, the op (byte 1) is below 5 and the length field equals the number of bytes received.

## Login handshake (new in fw ≥ 2023-02)

1. **Key exchange:** op 3 (SET) packet with one TLV, type **528**, value = RSA-encrypted session key, encrypted with the *static* RC4.
   - RSA is textbook (no padding), e = 65537, operands little-endian. The TL-SG108E v6 uses a **64-bit** modulus (see `internal/wire`); the vendor utility carries a 1024-bit modulus for some other models.
   - TLV value layout (10 bytes): `00`, number of 16-bit words (4), then the ciphertext as 4 little-endian 16-bit words, least significant word first.
2. **Reply:** op 4 with **error code 4098** means "session key accepted"; the reply is already encrypted with the session RC4.
3. **Login:** op 3 with TLV 512 (username) and TLV 514 (password), both NUL-terminated strings, session RC4. Error code **0** = success.
4. **Credentials ride on every SET**: the client prepends TLV 512/514 to each SET request. There is no token; a SET without valid credentials is rejected. **GET requests need no credentials** in this protocol version (the LED GET worked straight after the key exchange; whether it works without the key exchange was not tested).

## Device and session TLVs

| Type | Name | Dir | Bytes | Status |
|---|---|---|---|---|
| 1, 2, 3, 4, 5, 6, 7, 8, 9 | model, hostname, MAC, IP, mask, gateway, firmware, hardware, DHCP | GET (discovery) | | — |
| 14 | factory-default flag, `00` or `01` depending on the unit | GET (discovery) | 1 | — |
| **12** | **LED** (1 = on) | GET with an empty TLV; SET `01` / `00` | 1 | — |
| 10 | port count (login GET) | GET with an empty TLV | 1 | — |
| 512 / 514 | username / password | SET | NUL string | — |
| 528 | RSA-encrypted session key | SET | 10 | — |

Notes:

- **512 / 514:** the web UI limits the user name and the password to 16 characters each; whether the switch enforces it too is **not probed**, because a probe means writing the live account (a changed account can lock the user out for good), and the client does not write it.

## Settings TLVs

All GETs below need the session-key exchange first. Ask for one TLV type per GET: a GET that names several types at once is answered with error code 1 on this firmware (see the notes on IGMP and on features that are not active), so the client reads each setting separately. A few replies carry more than the type asked for (a GET of 4352 also returns 4354, a GET of 8448 returns 8448-8450). Multi-row settings come back as one TLV per row. Port numbers are 1-based; "mask" is a 32-bit big-endian bitmap, bit n-1 = port n.

### Port Setting (4096)

| Type | Dir | Bytes | Layout | Status |
|---|---|---|---|---|
| 4096 | GET | 7 per port | port, state(1=on), LAG id (0=none), cfg speed, actual speed, cfg flow control, actual flow control | — |
| 4096 | SET | 7 per port | port, state, 0, cfg speed, 0, flow control, 0 (the lag and actual-speed bytes zero) | verified live |

Speed: 0 link-down, 1 auto, 2 10M-half, 3 10M-full, 4 100M-half, 5 100M-full, 6 1000M-full.

Notes:

- One TLV per port; verified live on an unused port (flow control, speed, admin disable, all restored).
- **Flow control is kept only with speed auto or 1000M-full**: a write of 10 or 100 Mbps (half or full) with flow control on reads back with it off (firmware 20250710; the vendor utility enforces the same rule, "Failed to enable flow control when speed in Force mode"), so the client refuses that combination before sending.
- On a port that is a LAG member the switch refuses half duplex with code 7407 (see 4608).

### Port Trunk / LAG (4608)

| Type | Dir | Bytes | Layout | Status |
|---|---|---|---|---|
| 4608 | GET | 5 per group | group id (1 byte), member port mask (4 bytes); unused group = mask 0 | — |
| 4608 | SET | 5 | group id (1 byte), member port mask (4 bytes); mask 0 removes the group | verified live (20230218, 20250710) |

Notes:

- One TLV per write. Group 1 takes ports 1-4 and group 2 ports 5-8 (the web UI enforces the same ranges), two to four ports each.
- **Refusals:** code 4 ports outside the group's range, 7415 a single port, 7401 a port in a port-based VLAN other than the default, 7403 a bandwidth limit, 7404 storm control, 7205 the mirror destination, 7206 a mirrored port.
- **Member rules, enforced when the group is created** (firmware 20250710, probed on free ports): 7405 members have different QoS priorities (port-based QoS mode only), 7406 different fixed speeds, 7407 a member at half duplex, 7410 auto-negotiation mixed with a fixed speed (auto against 1000M-full is accepted), 7417 different flow control, 7418 not all enabled or all disabled.
- **After the group exists** the switch refuses only half duplex on a member (code 7407 from a port write); a different speed, flow control or admin state is accepted, so the members can drift apart.
- Not probed: the vendor utility's "MTU VLAN is enabled" rule.
- Verified live on firmware 20230218 (LAG2 = ports 6+7) and 20250710 (LAG1 = ports 2+3 and 2+4 and 3+4, created and removed through ESCP and the web UI).
- **A LAG write must respect the port ranges.** Group 1 only takes ports 1-4 and group 2 only ports 5-8, two to four members. Anything else is refused with code 4 (a single port with 7415). An early probe of firmware 20250710 mistook this for a firmware change because it used mixed ranges and group 3 while mirroring was on; the web UI's own script (`PortTrunkRpm.htm`) states the same ranges, and a switch on 20230218 shows them too.

### Port Statistics (16384)

| Type | Dir | Bytes | Layout | Status |
|---|---|---|---|---|
| 16384 | GET | 19 per port | port, state, link speed (as above), tx good, tx bad, rx good, rx bad: four 4-byte big-endian counters | — |

### Port Mirror (16640)

| Type | Dir | Bytes | Layout | Status |
|---|---|---|---|---|
| 16640 | GET | 10 | enabled, destination port, ingress mask(4), egress mask(4); all zero when disabled | disabled state verified live; enabled layout from vendor code |
| 16640 | SET | 10 | the same layout; all zero to disable | verified live (mirror port 4 -> unused port 7, then disabled) |

### QoS Basic (12288 / 12289)

| Type | Dir | Bytes | Layout | Status |
|---|---|---|---|---|
| 12288 | GET | 1 | mode: 0 port-based, 1 802.1p, 2 DSCP/802.1p | verified live |
| 12289 | GET | 2 per port | port(1) priority(1): raw 0..3, shown as 1 (lowest)..4 (highest) in the UI | verified live |
| 12288 | SET | 1 | mode | verified live (mode switched to port-based and back) |
| 12289 | SET | 2 per port | port, raw priority 0..3; one TLV for **every** port, as the vendor utility does | verified live (unused port) |

Notes:

- 12288 and 12289 are requested separately.
- **Priorities are accepted but ignored unless the mode is port-based (0).**

### Bandwidth Control (12544 / 12545)

| Type | Dir | Bytes | Layout | Status |
|---|---|---|---|---|
| 12544 / 12545 | GET | 6 per port | port(1), reserved(1), rate(4, kbps, 0 = unlimited); ingress / egress, requested separately | verified live (all unlimited) |
| 12544 / 12545 | SET | 6, one TLV per request | port(1), enable flag(1: 1 if rate != 0), rate(4, kbps); ingress SET first, then egress SET | verified live (unused port) |

Notes:

- The UI rounds the rate to multiples of 64, max 1000000. The "reserved" byte of the read is the enable flag of the write.
- **One limiter per port:** a SET with a rate other than 0 for the ingress TLV turns storm control off on that port (normal reply, no refusal); ingress 0 and the egress TLV leave it alone (probed on firmware 20250710, port 3, and on 20230218, port 7, 2026-10-27). `SetBandwidth` reads storm control first and refuses.

### Storm Control (12800)

| Type | Dir | Bytes | Layout | Status |
|---|---|---|---|---|
| 12800 | GET | 9 per port | port(1), reserved(1), unknown-unicast(1), multicast(1), broadcast(1), rate(4, kbps) | verified live (all off) |
| 12800 | SET | 9 per port | port, enable flag (1 if any type selected), unknown-unicast, multicast, broadcast flags, rate(4, kbps) | verified live (unused port) |

Notes:

- Active in the web UI when rate != 0 and a type is set. The rate is a multiple of 64 and >= 64 when a type is selected; 0 and no types disable it.
- **One limiter per port:** a SET with a traffic type selected removes the port's ingress limit (normal reply, no refusal); a SET with none selected leaves it alone (probed on firmware 20250710 and 20230218, 2026-10-27). `SetStormControl` reads the ingress limit first and refuses.

### IGMP Snooping (4352 / 4353 / 4354) and Loop Prevention (17152)

| Type | Dir | Bytes | Layout | Status |
|---|---|---|---|---|
| 4352 | GET, SET | 1 | snooping enabled | SET verified with a same-value write |
| 4354 | GET, SET | 1 | report message suppression; comes back with the 4352 GET | SET verified with a same-value write |
| 4353 | GET | 10 per entry | learned groups, repeated entries: ip(4) vlan(2) port mask(4) | — |
| 17152 | GET, SET | 1 | loop prevention, 1 = enabled; SET with credentials | SET verified with a same-value write |

Notes:

- **Request 4353 separately**: a combined GET of 4352+4354+4353 is rejected with error code 1. SET carries 4352 and 4354 together.

### VLANs: MTU (8192), Port Based (8448-8450) and 802.1Q (8704-8707)

The three VLAN modes (MTU, port-based, 802.1Q) exclude each other; the vendor utility warns of it.

| Type | Dir | Bytes | Layout | Status |
|---|---|---|---|---|
| 8192 | GET | 2 | MTU VLAN: enabled, uplink port (1 when disabled) | — |
| 8192 | SET | 2 | enabled, uplink port (the client sends 1 when disabling) | not exercised |
| 8448 | GET | 1 | port-based mode enabled | — |
| 8449 | GET | 5 per VLAN | id (1 byte), member port mask (4 bytes) | — |
| 8450 | GET | 1 | maximum number of VLANs (8 on a TL-SG108E) | — |
| 8448 | SET | 1 | port-based mode | verified live (20250710) |
| 8449 | SET | 5 | VLAN id (1 byte), member mask (4 bytes); mask 0 removes the group | verified live (VLAN 2 = ports 6+7) |
| 8704 | GET | 1 | 802.1Q mode | — |
| 8705 | GET | per VLAN | id(2) member mask(4) tagged mask(4) name (NUL string); tagged = member and in tagged mask, else untagged | layout from vendor code (a unit with 802.1Q off has none) |
| 8706 | GET | 3 per port | port(1) pvid(2) | verified live |
| 8707 | GET | 1 | maximum VLANs (32, VLAN 1 included) | — |
| 8704 | SET | 1 | 802.1Q mode (1 = on, 0 = off) | verified live (20250710) |
| 8705 | SET | per VLAN | id(2, big-endian), member mask(4) (tagged and untagged), tagged mask(4), name, NUL | verified live (20250710) |
| 8706 | SET | 3 per port | port(1), pvid(2, big-endian); several ports can go in one SET | verified live (20250710) |

Notes:

- **8192 (MTU VLAN):** with the mode on, every other port can only talk to the uplink port. TLVs 8708/8709 (MTU management ports) get no reply on this model. The write was not exercised live: it would isolate the ports and needs the uplink port to be right, which is too easy to get wrong on a switch with no console.
- **8448 (port-based mode write):** same mutual exclusion. Verified live (firmware 20250710): writing 1 turns 802.1Q off and restores the port-based group VLAN 1 with all eight ports (the 802.1Q VLANs are discarded).
- **8448 / 8449 / 8450 (reads):** one GET of 8448 returns all three. The web UI takes new port-based VLAN ids from 2 to the number in 8450 (its page uses the port count, which is the same 8), and the client refuses a group id above the larger of it and the port count before writing (read once and kept, so a model whose group limit is below its port count, such as a 24-port TL-SG1024DE might be, is not held to the smaller number). 8710/8711 (management ports) get no reply on this model.
- **The switch enforces the port-based group limit itself** (probed on firmware 20250710, 2026-10-30, port-based mode, a free port): a write of group 8 is accepted and reads back, a write of group 9 is refused with error code 7503 (the generic "wrong credentials, or the switch refuses this setting or value" code; the credentials were good), and removing a group 9 that does not exist is refused the same way, so the client's check is the switch's rule, not only the web UI's. On this 8-port model the group maximum and the port count are both 8, so the probe cannot say which of the two the firmware uses; the larger-of rule stays an inference for other models.
- **8449 (port-based group write):** **membership is exclusive**: adding ports to a group takes them out of their old group (VLAN 1), and removing the group returns them to VLAN 1 (verified live with VLAN 2 = ports 6+7). The mode (8448) is never written.
- **8704 (802.1Q mode write):** verified live (firmware 20250710): enabling it turns the port-based mode off and builds VLAN 1 (name `Default`) with every port untagged and every PVID 1; the management path through the uplink in VLAN 1 stayed up.
- **8705 (802.1Q VLAN write / delete):** a VLAN is deleted with both masks 0 and an empty name. Names are at most 10 characters of letters, digits, hyphen and underscore, ids 1-4094 (vendor utility rules). The 802.1Q mode must be on. Verified live (firmware 20250710): create with a tagged and an untagged port, read back identical, delete with the empty entry.
- **8706 (PVID write):** the VLAN must exist (vendor utility check) and the mode must be on. Verified live (firmware 20250710): PVID 20 on an unused port, back to 1. 8705 and 8706 are requested separately.
- **The switch enforces the 802.1Q table size itself** (probed on firmware 20250710, 2026-10-30, 802.1Q mode, VLANs tagged on a free port): with VLAN 1 there, 31 more VLANs were accepted and the 32nd addition was refused with error code 7505, so the table holds 32 entries in all; changing a VLAN that is already in a full table is not an addition. `Dot1Q.CheckRoom` lets a front end refuse a new VLAN in a full table before writing. 8708-8713 (management VLAN etc.) get no reply on this model.

### Config backup (768)

| Type | Dir | Bytes | Layout | Status |
|---|---|---|---|---|
| 768 | SET (read) | 2258 in all | an authenticated SET (user name 512 + password 514) carrying an **empty** TLV 768 returns the file in TLV 768 values | byte-identical to the web download; integrity measured on both builds (see the notes) |

Notes:

- The configuration file comes back in TLV 768 values spread over several packets. Every packet but the last has the header fragment field (bytes 22-23) set to 1. Total 2258 bytes, magic `23 89 23 89`, byte-identical to the web download. Nothing on the switch changes.
- **Integrity (measured 2026-10-27 on firmware 20230218 and 20250710):** the file travels as two packets, 1400 and 858 bytes of TLV 768; the header fields flag, token id and checksum are 0 in every reply, and the fragment field is only 1 on the first packet and 0 on the last (no index, no total); the file itself holds no length or checksum.
- The web UI download is TCP with `Content-Length: 2258`, so it cannot lose a piece; the vendor utility checks only that the length in each packet's header matches its TLVs and saves whatever reassembles (a lost first packet would give it the 858-byte tail).
- Over UDP a reply is lost now and then (2 of 228 downloads on this network: the last packet gone, 1400 bytes, and the first gone, 858 bytes). The client therefore accepts a download only when the file starts with the magic, the packets before the last are equally long and the last is not longer, and a second, separately requested download is byte for byte the same; otherwise it downloads again (`Config.Retries+1` rounds) and fails with `ErrInvalidConfigFile`.

### Device description and management address (2, 4, 5, 6, 9)

| Type | Dir | Bytes | Layout | Status |
|---|---|---|---|---|
| 2 | SET | NUL string | device description (hostname), sent with 9 (DHCP flag) and 4, 5, 6 (current IP, mask, gateway, 4 bytes each) as the vendor utility sends them | verified live |
| 4, 5, 6 | SET | 4 each | new IP, mask and gateway, with TLV 2 (name kept) and 9 (DHCP flag kept) | verified live (20250710) |
| 9 | SET | | DHCP flag; 1 asks the DHCP server for a lease, 0 with a static triple puts the switch back on that address | measured (20250710) |

Notes:

- **Device description (2):** renamed and restored, IP configuration unchanged. Letters, digits, hyphens only.
- **Management address (4, 5, 6):** the same SET as the device description. Verified live on firmware 20250710: the switch answers from the old or the new address (the client accepts both), takes about two seconds to move, and the configuration file is byte-identical after moving back. A first packet to an address the host has not yet ARP-resolved can fail with `host is down`, so the client polls discovery until the switch reports the new address.
- **DHCP flag (9):** the same SET, with TLV 9 = 1: the switch asks its DHCP server for a lease. Measured on firmware 20250710 (a router whose pool starts at .20): the write is answered; the switch is **silent for about two seconds**, then answers discovery at the **leased address** with DHCP 1 and the lease's mask and gateway (about four seconds after the write in total); the old static address no longer answers. Management works at the lease.
- **A static address written while DHCP is on is ignored** (the lease stays, flag still 1), and so is the address a device-description write carries. A SET with TLV 9 = 0 and a static triple puts the switch back on that address within a fraction of a second; the static triple is not remembered while it leases, so it must be sent again. The configuration file was byte-identical to the one before after DHCP on and off again.

### System extras (10, 11, 13, 14)

| Type | Dir | Bytes | Layout | Status |
|---|---|---|---|---|
| 10 | GET | 1 | port count | — |
| 13 | GET | 1 | auto-save (1 = on) | — |
| 14 | GET (discovery only) | 1 | factory-default flag | — |
| 11 | GET | | management VLAN | — (answers error -2 on this model) |

Notes:

- A GET of 14, 15 or 16 gets no reply.

## Caveats

- **No integrity or real confidentiality**: static key is public, the 64-bit RSA is trivially breakable, and a SET carries the password. Anyone on the LAN who sees the key exchange can recover the session key and the credentials. Treat ESCP as LAN-management-VLAN-only.
- Same-L2 only. macOS needs the interface bound for broadcast; unicast to the switch IP works too.

## Settings of a feature that is not active (measured 2026-10-26, firmware 20250710)

What the switch does when a value is written for a feature whose mode is off, read back immediately, after the mode is switched on, and after a round trip through the other VLAN mode. Test ports only, restored; the probe is `TestLiveInactiveSettings` (`-tags live`).

- **802.1Q VLAN (8705) and PVID (8706) while 802.1Q is off:** the write is **accepted** (no error code) and **not kept**. The switch reports no VLANs and every PVID 1 while the mode is off, and after the mode is switched on the table is the default only (VLAN 1, every port untagged, every PVID 1).
- **Port-based group (8449) while port-based mode is off:** accepted and **not kept**; after the mode is switched on only VLAN 1 with every port exists.
- **Tables do not survive a trip through the other mode.** A port-based group created in port-based mode is gone after 802.1Q has been switched on and off again (VLAN 1 with all ports is back), and an 802.1Q VLAN is gone after port-based mode has been on. Switching the VLAN mode **resets the table of the mode it switches to**.
- **QoS port priorities while the QoS mode is not port-based:** accepted, **not kept**: the priority reads back as before, also after the mode is switched to port-based.
- **A disabled port-mirror entry that carries ports** (enabled 0, destination and source set): accepted, **not kept**: the entry reads back as all zeros.
- **IGMP report suppression while snooping is off:** accepted and **kept**; it reads back and is unchanged after snooping is switched on. This is the one value that can be staged.
- Not probed: the MTU VLAN uplink port while the mode is off (a misread write could enable the mode without its uplink and cut the management path).
- A GET that asks for several TLV types at once answers error code 1 on this firmware whatever the mode (as already noted for IGMP); read each setting separately.

## Compatibility notes

- Verified on hardware v6, firmware **20230218** and **20250710**: the same RSA key exchange and login, the same TLV layouts, and the same replies to every read (a read-only probe of 39 unreviewed TLV ids answered identically, all silent). Other Easy Smart models are not verified; try the reads (`Info`, `Ports`) on a spare unit first.
- A rejected write is reported as `escp: switch rejected the request (error code N)`. Code 4 is not a credentials error: the credentials are in every write and the other writes on the same switch succeeded.
- Only GETs are safe to experiment with: an unknown TLV is usually answered with silence or an error code, but a SET can change the device.
