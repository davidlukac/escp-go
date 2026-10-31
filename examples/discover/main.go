// Command discover is a read-only example of the library: it lists the switches that answer a
// discovery probe on the local network and, when ESCP_HOST, ESCP_USERNAME and ESCP_PASSWORD are
// set, the ports of that switch. It changes nothing on any switch.
//
//	go run ./examples/discover
//	ESCP_HOST=192.0.2.11 ESCP_USERNAME=admin ESCP_PASSWORD=... go run ./examples/discover
//
// Run it on its own: switches reply to UDP port 29809, which the vendor's utility on the same host
// would share out unpredictably.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/davidlukac/escp-go"
)

// main is the example's whole program; it has no logic worth testing apart from the library calls.
//
// coverage: ignore - example program that talks to the real network
func main() {
	iface := flag.String("interface", "", "restrict discovery to this network interface (default: all)")
	window := flag.Duration("window", 2*time.Second, "how long to listen for replies")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *window+5*time.Second)
	defer cancel()

	switches, err := escp.Discover(ctx, escp.DiscoverOptions{Interface: *iface, Window: *window})
	if err != nil {
		fmt.Fprintln(os.Stderr, "discovery stopped early:", err)
	}
	if len(switches) == 0 {
		fmt.Println("no switch answered")
	}
	for _, s := range switches {
		fmt.Printf("%-15s %s  %s  firmware %s\n", s.IP, s.MAC, s.Hardware, s.Firmware)
	}

	host := os.Getenv("ESCP_HOST")
	if host == "" {
		return
	}
	c, err := escp.Dial(escp.Config{
		Host:     host,
		Username: os.Getenv("ESCP_USERNAME"),
		Password: escp.Password(os.Getenv("ESCP_PASSWORD")),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = c.Close() }() // nothing was written, so a failed close changes nothing
	ports, err := c.Ports(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "reading the ports:", err)
		os.Exit(1)
	}
	for _, p := range ports {
		fmt.Printf("port %d: %-14s (configured %s)\n", p.ID, p.ActualSpeed, p.ConfiguredSpeed)
	}
}
