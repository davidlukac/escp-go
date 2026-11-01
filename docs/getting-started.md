# Getting started

```sh
go get github.com/davidlukac/escp-go
```

```go
import (
	"github.com/davidlukac/escp-go"        // package escp: the client
	"github.com/davidlukac/escp-go/safety" // write gates and the compatibility table
)
```

The module needs Go 1.26 or later. Pin an exact version while it is 0.x ([CHANGELOG.md](../CHANGELOG.md) lists every change).

> **The switch is real network equipment with no console.** A wrong write can cut the management path or the uplink, and recovery is a physical factory reset. Read [Writing safely](writing-safely.md) before the first write.

## Connect and read

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

- `Dial` sends nothing and binds no socket; the first call does the key exchange.
- Every method that talks to the switch takes a `context.Context`. Canceling it stops the call, and nothing is written once it is done.
- **One `Client` per switch.** A `Client` is safe for concurrent use but sends one request at a time, because the switch answers one at a time. Clients for different switches work side by side, in one process or on one host. Two clients for the same switch do not work: the switch keeps one session, so each key exchange invalidates the other client's.
- Switches always reply to UDP port 29809 whoever asked, so every client on a host shares that port and each keeps only the replies from its own switch. The library sets up the sharing on Linux, macOS, FreeBSD and Windows (observed with real switches on macOS; the tests show a broadcast reaching every sharing socket on Linux); on any other system the second bind fails, so only one client per host works.
- The reads are named for what they return (`Ports`, `QoS`, `LED`, `Dot1Q`); the writes are `Set...` and `Delete...`. The table in the [README](../README.md#what-it-can-read-and-write) lists them all.

## Find switches

`Discover` finds the switches on the local network without credentials or addresses. Run it on its own: it listens on UDP port 29809, which a `Client` or the vendor's utility on the same host would share out unpredictably.

```go
switches, err := escp.Discover(ctx, escp.DiscoverOptions{Window: 2 * time.Second})
for _, s := range switches {
	fmt.Println(s.IP, s.MAC, s.Hardware, s.Firmware)
}
```

It probes the subnet broadcast address of each usable interface (`DiscoverOptions.Interface` restricts it to one), repeats the probe halfway through the window, and returns one `Info` per MAC address, sorted by IP address; an empty list, not an error, when nobody answers.

## Find a switch by its MAC address

A switch's address can change (a lease, a move); its MAC address does not. Name it in the configuration and the client pins the identity and finds the switch when it has to:

```go
mac, _ := escp.ParseMAC("00:00:5e:00:53:01") // escp.MAC: a comparable value; the zero value means "no MAC"
c, err := escp.Dial(escp.Config{MAC: mac, Username: user, Password: escp.Password(pw)}) // no Host: found on the first request
```

- With `Config.MAC` set, a device that answers with another MAC is refused (`ErrWrongSwitch`, naming both) and nothing is written to it. The MAC is checked on every reply, the key exchange's included.
- With no `Host`, or a `Host` that stops answering, the client looks the switch up by its MAC: a discovery probe that stops at the first reply from that switch, normally under a second. A lookup that finds nobody is tried once more before `ErrNotFound` is returned, because one right after a management move once missed a switch that was there. `escp.Locate(ctx, mac, opts)` is the same lookup for a front end.
- A device that takes over the address meanwhile ignores requests encrypted with a session key it never accepted; the client then starts a new session, and the reply to the key exchange names the stranger, so nothing is written to it.
- `Dial` still sends nothing; the lookup happens on the first request.

## Runnable examples

The examples in the package documentation run against the [fake switch](testing.md) and show their output; [`examples/discover`](../examples/discover/main.go) is a small read-only program for a real network.
