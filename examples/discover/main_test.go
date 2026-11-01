package main

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/davidlukac/escp-go"
	"github.com/davidlukac/escp-go/escptest"
)

var errNoNetwork = errors.New("no network")

// program runs the example against a fake switch: discovery finds what the test says, and the client
// it dials talks to the fake on loopback.
type program struct {
	found    []escp.Info
	discover error
	env      map[string]string
	sw       *escptest.Switch
}

func (p program) run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	d := deps{
		discover: func(context.Context, escp.DiscoverOptions) ([]escp.Info, error) { return p.found, p.discover },
		dial: func(cfg escp.Config) (*escp.Client, error) {
			cfg.Host, cfg.Timeout, cfg.Retries = "127.0.0.1", 200*time.Millisecond, escp.NoRetries
			cfg.SwitchPort, cfg.HostPort, cfg.BroadcastAddr = p.sw.UDPPort(), escptest.FreeUDPPort(t), escptest.Loopback()
			return escp.Dial(cfg)
		},
	}
	code = run(args, func(k string) string { return p.env[k] }, &out, &errOut, d)
	return code, out.String(), errOut.String()
}

func TestWithoutAHostItOnlyListsTheSwitches(t *testing.T) {
	sw := escptest.Start(t)
	found := []escp.Info{{IP: netip.MustParseAddr("192.0.2.11"), MAC: escp.MAC{2, 0, 0, 0, 0, 1}, Hardware: "TL-SG108E 6.0", Firmware: escp.ParseFirmware("1.0.0 Build 20230218 Rel.50633")}}
	code, out, errOut := program{found: found, sw: sw}.run(t)
	if code != exitOK || errOut != "" || !strings.Contains(out, "192.0.2.11") || !strings.Contains(out, "02:00:00:00:00:01") || strings.Contains(out, "port ") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
	if sw.Received() != 0 {
		t.Fatal("without ESCP_HOST nothing may be sent to a switch")
	}
}

func TestNobodyAnsweringIsSaidSo(t *testing.T) {
	code, out, errOut := program{sw: escptest.Start(t)}.run(t, "-window", "1s", "-interface", "en0")
	if code != exitOK || !strings.Contains(out, "no switch answered") || errOut != "" {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestADiscoveryErrorIsReportedAndTheRunGoesOn(t *testing.T) {
	code, out, errOut := program{sw: escptest.Start(t), discover: errNoNetwork}.run(t)
	if code != exitOK || !strings.Contains(errOut, "discovery stopped early: no network") || !strings.Contains(out, "no switch answered") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestWithAHostItPrintsThePorts(t *testing.T) {
	sw := escptest.Start(t)
	sw.EnableModel(map[int]escptest.LinkSpeed{1: escptest.Link1000Full})
	env := map[string]string{"ESCP_HOST": "192.0.2.11", "ESCP_USERNAME": escptest.Username, "ESCP_PASSWORD": escptest.Password}
	code, out, errOut := program{sw: sw, env: env}.run(t)
	if code != exitOK || errOut != "" || !strings.Contains(out, "port 1: 1000Mbps-full") || !strings.Contains(out, "port 8: link-down") {
		t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
	}
	if sw.WriteCount() != 0 {
		t.Fatal("the example is read-only")
	}
}

func TestASwitchThatDoesNotAnswerIsAnError(t *testing.T) {
	sw := escptest.Start(t)
	sw.Mute()
	code, _, errOut := program{sw: sw, env: map[string]string{"ESCP_HOST": "192.0.2.11"}}.run(t)
	if code != exitError || !strings.Contains(errOut, "reading the ports:") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
}

func TestADialErrorIsAnError(t *testing.T) {
	var out, errOut bytes.Buffer
	d := deps{
		discover: func(context.Context, escp.DiscoverOptions) ([]escp.Info, error) { return nil, nil },
		dial:     func(escp.Config) (*escp.Client, error) { return nil, errNoNetwork },
	}
	code := run(nil, func(k string) string { return map[string]string{"ESCP_HOST": "x"}[k] }, &out, &errOut, d)
	if code != exitError || !strings.Contains(errOut.String(), "no network") {
		t.Fatalf("code %d, stderr %q", code, errOut.String())
	}
}

func TestABadFlagIsAUsageError(t *testing.T) {
	code, _, errOut := program{sw: escptest.Start(t)}.run(t, "-nonsense")
	if code != exitUsage || !strings.Contains(errOut, "nonsense") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
}
