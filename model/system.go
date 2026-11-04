package model

import (
	"regexp"
)

// System holds device facts that are not part of discovery.
type System struct {
	PortCount int
	AutoSave  bool
}

// Hostname is the device description of a switch (System > System Info), what the web UI calls the
// device description and the protocol the hostname: 1 to MaxHostnameLen letters, digits and
// hyphens, not starting or ending with a hyphen. The rules are the web UI's.
type Hostname string

var hostnameRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?$`)

// Validate applies the rules; the error matches ErrInvalid.
func (h Hostname) Validate() error {
	if len(h) > MaxHostnameLen || !hostnameRE.MatchString(string(h)) {
		return invalidf("escp: invalid device description %q: 1-%d letters, digits and hyphens, not starting or ending with a hyphen", string(h), MaxHostnameLen)
	}
	return nil
}

// String is the device description.
func (h Hostname) String() string { return string(h) }
