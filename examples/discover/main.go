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
	"io"
	"os"
	"time"

	"github.com/davidlukac/escp-go"
)

// Exit codes of the program.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// extraWait is how long the whole run may take beyond the discovery window.
const extraWait = 5 * time.Second

// deps are the two things that reach the network, so that a test can aim them at a fake switch.
type deps struct {
	discover func(context.Context, escp.DiscoverOptions) ([]escp.Info, error)
	dial     func(escp.Config) (*escp.Client, error)
}

// main is the process entry point.
//
// coverage: ignore - process entry point; run holds all the logic and is tested.
func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr, deps{discover: escp.Discover, dial: escp.Dial}))
}

// run is the whole program: args are the command line without the program name, getenv reads the
// ESCP_* variables.
func run(args []string, getenv func(string) string, stdout, stderr io.Writer, d deps) int {
	flags := flag.NewFlagSet("discover", flag.ContinueOnError)
	flags.SetOutput(stderr)
	iface := flags.String("interface", "", "restrict discovery to this network interface (default: all)")
	window := flags.Duration("window", 2*time.Second, "how long to listen for replies")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}

	ctx, cancel := context.WithTimeout(context.Background(), *window+extraWait)
	defer cancel()

	switches, err := d.discover(ctx, escp.DiscoverOptions{Interface: *iface, Window: *window})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "discovery stopped early:", err)
	}
	if len(switches) == 0 {
		_, _ = fmt.Fprintln(stdout, "no switch answered")
	}
	for _, s := range switches {
		_, _ = fmt.Fprintf(stdout, "%-15s %s  %s  firmware %s\n", s.IP, s.MAC, s.Hardware, s.Firmware)
	}

	host := getenv("ESCP_HOST")
	if host == "" {
		return exitOK
	}
	return printPorts(ctx, d, escp.Config{Host: host, Username: getenv("ESCP_USERNAME"), Password: escp.Password(getenv("ESCP_PASSWORD"))}, stdout, stderr)
}

// printPorts reads the ports of the switch cfg names and prints them.
func printPorts(ctx context.Context, d deps, cfg escp.Config, stdout, stderr io.Writer) int {
	c, err := d.dial(cfg)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return exitError
	}
	defer func() { _ = c.Close() }() // nothing was written, so a failed close changes nothing
	ports, err := c.Ports(ctx)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "reading the ports:", err)
		return exitError
	}
	for _, p := range ports {
		_, _ = fmt.Fprintf(stdout, "port %d: %-14s (configured %s)\n", p.ID, p.ActualSpeed, p.ConfiguredSpeed)
	}
	return exitOK
}
