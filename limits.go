package escp

import "strconv"

const (
	// MaxPort is the highest port number a 32-bit port mask can express.
	MaxPort = 32
	// MaxHostnameLen is the longest device description the switch accepts.
	MaxHostnameLen = 32
	// maskLen is the size of a port mask on the wire.
	maskLen = 4
)

// The limits of a rate limit.
const (
	// RateStep is the granularity of rate limits: the switch rounds to multiples of 64 kbps.
	RateStep Kbps = 64
	// MaxBandwidth is the largest limit the web UI offers.
	MaxBandwidth Kbps = 1000000
)

// Kbps is a rate limit in kilobits per second, as the bandwidth and storm control pages take it:
// 0 is unlimited (or, for storm control, no limit set), otherwise a multiple of RateStep up to
// MaxBandwidth. It is the unit of the switch's own rate fields.
type Kbps int

// Validate reports whether k is a rate the switch can hold; the error matches ErrInvalid and says
// what is allowed.
func (k Kbps) Validate() error {
	if k < 0 || k > MaxBandwidth || k%RateStep != 0 {
		return invalidf("escp: %d kbps is not a valid rate: use 0 (unlimited) or a multiple of %d up to %d", k, RateStep, MaxBandwidth)
	}
	return nil
}

// String is the rate with its unit, "128 kbps".
func (k Kbps) String() string { return strconv.Itoa(int(k)) + " kbps" }
