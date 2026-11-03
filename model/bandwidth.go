package model

// PortBandwidth holds a port's rate limits in kbps; 0 means unlimited.
type PortBandwidth struct {
	Port    PortID
	Ingress Kbps
	Egress  Kbps
}

// Validate checks the limits before they are written: a valid port and, in each direction, 0
// (unlimited) or a multiple of RateStep up to MaxBandwidth. The error matches ErrInvalid.
func (b PortBandwidth) Validate() error {
	if err := b.Port.Validate(); err != nil {
		return err
	}
	for _, rate := range []Kbps{b.Ingress, b.Egress} {
		if rate.Validate() != nil {
			return invalidf("escp: invalid rate %d kbps for port %d: use 0 (unlimited) or a multiple of %d up to %d", rate, b.Port, RateStep, MaxBandwidth)
		}
	}
	return nil
}
