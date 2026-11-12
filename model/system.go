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
		return invalidf("escp: invalid device description %s: 1-%d letters, digits and hyphens, not starting or ending with a hyphen", quoteBounded(string(h)), MaxHostnameLen)
	}
	return nil
}

// UnmarshalText reads a device description with the rule of Validate, so that a decoder (encoding/json
// for a string, a map key, a configuration file) refuses what a writer refuses; the error is
// Validate's and matches ErrInvalid, and the receiver is left alone on a refusal. The empty text is
// the zero value, "not set" (a reply that carried no description decodes as one), which Validate
// still refuses as a setting.
func (h *Hostname) UnmarshalText(text []byte) error {
	v := Hostname(text)
	if len(v) > 0 {
		if err := v.Validate(); err != nil {
			return err
		}
	}
	*h = v
	return nil
}

// String is the device description.
func (h Hostname) String() string { return string(h) }
