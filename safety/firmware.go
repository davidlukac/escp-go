package safety

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/davidlukac/escp-go/model"
)

// verifiedFirmwareBuilds is the allow-list behind VerifiedFirmwareBuilds: the builds of the
// verified rows of the compatibility table (compatibility.json). It is unexported so that no
// code in the process can append to it and weaken the gate.
var verifiedFirmwareBuilds = verifiedBuilds(compatibility)

// VerifiedFirmwareBuilds returns the firmware builds (the date in "1.0.0 Build
// 20230218 Rel.50633") whose protocol behavior this client was tested
// against on real switches: reads, and writes on unused ports. Any other build
// may have changed layouts or authentication, so writes to it are refused
// unless the user opts in. The result is a copy: changing it changes nothing.
func VerifiedFirmwareBuilds() []model.BuildDate { return slices.Clone(verifiedFirmwareBuilds) }

// FirmwareReason says why a firmware check refused a write.
type FirmwareReason int

// The reasons a firmware check refuses.
const (
	// FirmwareUnreadable: the firmware version could not be read from the switch.
	FirmwareUnreadable FirmwareReason = iota + 1
	// FirmwareNoBuildDate: the version string has no build date to match.
	FirmwareNoBuildDate
	// FirmwareUnverified: the build is not in VerifiedFirmwareBuilds.
	FirmwareUnverified
)

// FirmwareError is the refusal of a firmware check. Callers branch on Reason and
// word the message for their users; Error is a neutral description.
type FirmwareError struct {
	Reason   FirmwareReason
	Firmware model.Firmware    // the firmware the switch reported, its build date included (not for FirmwareUnreadable)
	Verified []model.BuildDate // the builds that would have been accepted
	Err      error             // the cause (FirmwareUnreadable)
}

// Error describes the refusal in neutral words (which build, what would have been accepted); front
// ends word their own message from Reason.
func (e *FirmwareError) Error() string {
	switch e.Reason {
	case FirmwareUnreadable:
		return fmt.Sprintf("cannot read the firmware version to check it: %v", e.Err)
	case FirmwareNoBuildDate:
		return fmt.Sprintf("the firmware version %q has no build date, so it cannot be matched against the verified builds %v", e.Firmware, e.Verified)
	default:
		return fmt.Sprintf("firmware build %s has not been verified with this version of the client (verified: %v)", e.Firmware.BuildDate(), e.Verified)
	}
}

// Unwrap returns the cause of a FirmwareUnreadable refusal.
func (e *FirmwareError) Unwrap() error { return e.Err }

// CheckFirmware returns a *FirmwareError unless the firmware version names
// a build in VerifiedFirmwareBuilds.
func CheckFirmware(fw model.Firmware) error {
	if !fw.HasBuild() {
		return &FirmwareError{Reason: FirmwareNoBuildDate, Firmware: fw, Verified: VerifiedFirmwareBuilds()}
	}
	if !slices.Contains(verifiedFirmwareBuilds, fw.BuildDate()) {
		return &FirmwareError{Reason: FirmwareUnverified, Firmware: fw, Verified: VerifiedFirmwareBuilds()}
	}
	return nil
}

// FirmwareGate returns an escp.WriteGate that refuses every write while the
// switch runs a firmware build the client was not verified against. Reads always
// pass: they cannot change the switch, and they are how a new firmware is
// fingerprinted. The firmware is read before the first write to a switch; a verified build
// is remembered for that switch, by its MAC address (Reader.SwitchMAC), a refusal or a failed read
// is not (the next write checks again). A gate may therefore be shared by clients of several
// switches, each is checked once; a reader that does not know its switch's MAC is checked at every
// write.
func FirmwareGate() model.WriteGate {
	g := &firmwareGate{verified: map[model.MAC]bool{}}
	return g.check
}

type firmwareGate struct {
	mu       sync.Mutex
	verified map[model.MAC]bool // the switches whose build was found verified
}

func (g *firmwareGate) check(ctx context.Context, sw model.Reader, _ model.Operation) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if mac := sw.SwitchMAC(); !mac.IsZero() && g.verified[mac] {
		return nil
	}
	info, err := sw.Info(ctx)
	if err != nil {
		return &FirmwareError{Reason: FirmwareUnreadable, Err: err}
	}
	if err := CheckFirmware(info.Firmware); err != nil {
		return err
	}
	// Info is the first contact of a client that had none, so the MAC is known now. A reader that
	// still reports none (a double) is checked again at every write rather than remembered.
	if mac := sw.SwitchMAC(); !mac.IsZero() {
		g.verified[mac] = true
	}
	return nil
}
