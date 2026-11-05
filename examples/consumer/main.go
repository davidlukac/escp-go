// Command consumer is the program that release.yml builds in a fresh module against the published
// version of the library: it uses one package of each kind (the client, the values, the gates and
// the fake switch), so a version that cannot be installed, or that lacks a package or an
// identifier, fails the release. It is also a program of this module, so every pull request
// compiles, vets and lints it, and a change of the exported API that breaks it is seen there and
// not after the tag.
//
//	go run ./examples/consumer
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/davidlukac/escp-go"
	"github.com/davidlukac/escp-go/escptest"
	"github.com/davidlukac/escp-go/model"
	"github.com/davidlukac/escp-go/safety"
)

// sampleMAC is the address the program parses: the documentation range of MAC addresses.
const sampleMAC = "00:00:5e:00:53:01"

// main is the process entry point.
//
// coverage: ignore - process entry point; run holds all the logic and is tested.
func main() {
	if err := run(os.Stdout, sampleMAC); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run parses mac with the values package and prints it with one identifier of each of the other
// packages.
func run(w io.Writer, mac string) error {
	parsed, err := model.ParseMAC(mac)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, parsed, safety.FirmwareGate() != nil, escptest.Username, escp.MaxTimeout)
	return err
}
