package safety

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/davidlukac/escp-go/model"
)

func TestCheckFirmware(t *testing.T) {
	for _, v := range []string{"1.0.0 Build 20230218 Rel.50633", "1.0.0 Build 20250710 Rel.71093"} {
		t.Run(v, func(t *testing.T) {
			if err := CheckFirmware(model.ParseFirmware(v)); err != nil {
				t.Errorf("%q must be verified: %v", v, err)
			}
		})
	}
	tests := []struct {
		version, frag string
		reason        FirmwareReason
		build         model.BuildDate
	}{
		{"1.0.0 Build 20260101 Rel.1", "build 20260101 has not been verified", FirmwareUnverified, "20260101"},
		{"1.0.0 Build 20220930 Rel.1", "build 20220930 has not been verified", FirmwareUnverified, "20220930"},
		{"unknown", "has no build date", FirmwareNoBuildDate, ""},
		{"", "has no build date", FirmwareNoBuildDate, ""},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			err := CheckFirmware(model.ParseFirmware(tt.version))
			var fe *FirmwareError
			if !errors.As(err, &fe) || fe.Reason != tt.reason || fe.Firmware.Build != tt.build || fe.Firmware != model.ParseFirmware(tt.version) || !strings.Contains(err.Error(), tt.frag) {
				t.Errorf("CheckFirmware(%q) = %#v, want reason %d build %q containing %q", tt.version, err, tt.reason, tt.build, tt.frag)
			}
			if !slices.Equal(fe.Verified, VerifiedFirmwareBuilds()) {
				t.Errorf("CheckFirmware(%q) lists %v, want %v", tt.version, fe.Verified, VerifiedFirmwareBuilds())
			}
		})
	}
}

// The verified list in an error is a copy: a caller cannot change the allow-list through it.
func TestFirmwareErrorDoesNotExposeTheAllowList(t *testing.T) {
	var fe *FirmwareError
	if !errors.As(CheckFirmware(model.ParseFirmware("1.0.0 Build 20990101 Rel.1")), &fe) {
		t.Fatal("want a FirmwareError")
	}
	fe.Verified[0] = "tampered"
	if VerifiedFirmwareBuilds()[0] == "tampered" {
		t.Fatal("the allow-list was modified through the error")
	}
}

// The accessor hands out a copy too, and the allow-list is what the gate decides by.
func TestVerifiedFirmwareBuildsIsACopy(t *testing.T) {
	got := VerifiedFirmwareBuilds()
	if !slices.Contains(got, "20230218") || !slices.Contains(got, "20250710") {
		t.Fatalf("the two builds tested on real switches must be listed, got %v", got)
	}
	got[0] = "20990101" // overwrite an entry of the copy with a build nobody verified
	if err := CheckFirmware(model.ParseFirmware("1.0.0 Build 20990101 Rel.1")); err == nil {
		t.Error("a build written into the copy passed the gate")
	}
	if err := CheckFirmware(model.ParseFirmware("1.0.0 Build 20230218 Rel.50633")); err != nil {
		t.Errorf("a verified build was refused after the copy was changed: %v", err)
	}
}

// infoSource is a switch that reports a firmware version, or fails to.
type infoSource struct {
	model.Reader
	firmware string
	err      error
	reads    int
	mac      model.MAC // zero: a reader that does not know its switch
}

func (s *infoSource) Info(context.Context) (model.Info, error) {
	s.reads++
	return model.Info{Firmware: model.ParseFirmware(s.firmware), MAC: s.mac}, s.err
}

func (s *infoSource) SwitchMAC() model.MAC { return s.mac }

func TestFirmwareGateAllowsVerifiedBuildsAndReadsTheFirmwareOnce(t *testing.T) {
	for _, version := range []string{"1.0.0 Build 20230218 Rel.50633", "1.0.0 Build 20250710 Rel.71093"} {
		t.Run(version, func(t *testing.T) {
			sw := &infoSource{firmware: version, mac: model.MAC{0, 0, 0x5e, 0, 0x53, 1}}
			gate := FirmwareGate()
			for range 3 {
				if err := gate(context.Background(), sw, model.Operation{}); err != nil {
					t.Fatalf("%s: %v", version, err)
				}
			}
			if sw.reads != 1 {
				t.Fatalf("%s: want one firmware read for three writes, got %d", version, sw.reads)
			}
		})
	}
}

func TestFirmwareGateRefusesAnUnverifiedBuildAndDoesNotRememberTheRefusal(t *testing.T) {
	sw := &infoSource{firmware: "1.0.0 Build 20990101 Rel.1"}
	gate := FirmwareGate()
	var fe *FirmwareError
	if err := gate(context.Background(), sw, model.Operation{}); !errors.As(err, &fe) || fe.Reason != FirmwareUnverified || fe.Firmware.Build != "20990101" {
		t.Fatalf("got %v", err)
	}
	sw.firmware = "1.0.0 Build 20230218 Rel.50633" // the switch now reports a verified build
	if err := gate(context.Background(), sw, model.Operation{}); err != nil {
		t.Fatalf("a refusal must not stick: %v", err)
	}
}

func TestFirmwareGateRefusesWhenTheFirmwareCannotBeRead(t *testing.T) {
	cause := errors.New("no reply")
	sw := &infoSource{firmware: "1.0.0 Build 20230218 Rel.50633", err: cause}
	gate := FirmwareGate()
	var fe *FirmwareError
	err := gate(context.Background(), sw, model.Operation{})
	if !errors.As(err, &fe) || fe.Reason != FirmwareUnreadable || !errors.Is(err, cause) || !strings.Contains(err.Error(), "cannot read the firmware version") {
		t.Fatalf("got %v", err)
	}
	sw.err = nil
	if err := gate(context.Background(), sw, model.Operation{}); err != nil {
		t.Fatalf("the check is retried once the switch answers: %v", err)
	}
}

func TestFirmwareGateRefusesAFirmwareWithoutABuildDate(t *testing.T) {
	var fe *FirmwareError
	if err := FirmwareGate()(context.Background(), &infoSource{firmware: "custom"}, model.Operation{}); !errors.As(err, &fe) || fe.Reason != FirmwareNoBuildDate {
		t.Fatalf("got %v", err)
	}
}

// One gate may serve several switches: it remembers the verdict per switch, so a verified one does
// not vouch for another that runs a build nobody verified.
func TestAGateSharedBetweenSwitchesChecksEachOnce(t *testing.T) {
	good := &infoSource{firmware: "1.0.0 Build 20230218 Rel.50633", mac: model.MAC{0, 0, 0x5e, 0, 0x53, 1}}
	bad := &infoSource{firmware: "1.0.0 Build 20990101 Rel.1", mac: model.MAC{0, 0, 0x5e, 0, 0x53, 2}}
	gate := FirmwareGate()
	ctx := context.Background()
	for range 2 {
		if err := gate(ctx, good, model.Operation{}); err != nil {
			t.Fatal(err)
		}
	}
	var fe *FirmwareError
	if err := gate(ctx, bad, model.Operation{}); !errors.As(err, &fe) || fe.Reason != FirmwareUnverified {
		t.Fatalf("the second switch must be checked on its own: %v", err)
	}
	if good.reads != 1 || bad.reads != 1 {
		t.Fatalf("reads: good %d, bad %d, want one each", good.reads, bad.reads)
	}
}

// The same switch behind a new address or a new client is still the same switch.
func TestAVerifiedSwitchIsRememberedAcrossReaders(t *testing.T) {
	mac := model.MAC{0, 0, 0x5e, 0, 0x53, 1}
	first := &infoSource{firmware: "1.0.0 Build 20230218 Rel.50633", mac: mac}
	second := &infoSource{firmware: "1.0.0 Build 20230218 Rel.50633", mac: mac}
	gate := FirmwareGate()
	// subtests: two readers in sequence, the second depends on what the first left in the gate
	for _, sw := range []*infoSource{first, second} {
		if err := gate(context.Background(), sw, model.Operation{}); err != nil {
			t.Fatal(err)
		}
	}
	if second.reads != 0 {
		t.Fatalf("the switch was verified already, got %d reads", second.reads)
	}
}

// A reader that does not know its switch cannot be remembered: it is checked at every write.
func TestAReaderWithoutAMACIsCheckedAtEveryWrite(t *testing.T) {
	sw := &infoSource{firmware: "1.0.0 Build 20230218 Rel.50633"}
	gate := FirmwareGate()
	for range 3 {
		if err := gate(context.Background(), sw, model.Operation{}); err != nil {
			t.Fatal(err)
		}
	}
	if sw.reads != 3 {
		t.Fatalf("got %d reads for three writes, want 3", sw.reads)
	}
}

// Each gate owns its verdict: a verified switch must not vouch for another one.
func TestFirmwareGatesDoNotShareState(t *testing.T) {
	good := &infoSource{firmware: "1.0.0 Build 20230218 Rel.50633"}
	bad := &infoSource{firmware: "1.0.0 Build 20990101 Rel.1"}
	a, b := FirmwareGate(), FirmwareGate()
	if err := a(context.Background(), good, model.Operation{}); err != nil {
		t.Fatal(err)
	}
	if err := b(context.Background(), bad, model.Operation{}); err == nil {
		t.Fatal("the second gate must check its own switch")
	}
}

// The zero value of a reason is no reason: a FirmwareError built without one cannot pass for a
// refusal of a particular kind.
func TestTheZeroFirmwareReasonIsNoneOfTheReasons(t *testing.T) {
	var unset FirmwareReason
	for _, r := range []FirmwareReason{FirmwareUnreadable, FirmwareNoBuildDate, FirmwareUnverified} {
		t.Run(fmt.Sprint(r), func(t *testing.T) {
			if r == unset {
				t.Errorf("reason %d is the zero value", r)
			}
		})
	}
}
