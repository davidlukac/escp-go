package model

// PortStorm is a port's storm control setting: which kinds of traffic are limited and at what rate.
type PortStorm struct {
	Port           PortID
	UnknownUnicast bool // limit unknown-unicast traffic
	Multicast      bool // limit multicast traffic
	Broadcast      bool // limit broadcast traffic
	Rate           Kbps // the limit, a multiple of RateStep; 0 when none is set
	// Enabled mirrors the web UI: a rate is set and at least one traffic type is selected.
	Enabled bool
}

// StormConfig is what SetStormControl writes to one port. Unlike PortStorm (a reading)
// it has no Enabled flag: storm control is on exactly when a traffic type is selected.
type StormConfig struct {
	Port           PortID
	UnknownUnicast bool
	Multicast      bool
	Broadcast      bool
	Rate           Kbps
}

// Limited reports whether any traffic type is selected: storm control is on exactly then.
func (cfg StormConfig) Limited() bool { return cfg.UnknownUnicast || cfg.Multicast || cfg.Broadcast }

// Validate checks the setting before it is written: a valid port, and either a traffic type with a
// rate (a multiple of RateStep, at least one step, at most MaxBandwidth) or no traffic type
// and no rate. The error matches ErrInvalid.
func (cfg StormConfig) Validate() error {
	if err := cfg.Port.Validate(); err != nil {
		return err
	}
	switch {
	case cfg.Limited() && (cfg.Rate < RateStep || cfg.Rate.Validate() != nil):
		return invalidf("escp: storm control on port %d needs a rate that is a multiple of %d kbps between %d and %d, got %d", cfg.Port, RateStep, RateStep, MaxBandwidth, cfg.Rate)
	case !cfg.Limited() && cfg.Rate != 0:
		return invalidf("escp: storm control on port %d has a rate (%d) but no traffic type selected", cfg.Port, cfg.Rate)
	}
	return nil
}
