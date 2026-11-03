package model

// PortStats is one row of Monitoring > Port Statistics. Counters are the
// switch's 32-bit packet counters and wrap.
type PortStats struct {
	ID      PortID
	Enabled bool
	Link    Speed // SpeedLinkDown or the negotiated speed
	TxGood  uint32
	TxBad   uint32
	RxGood  uint32
	RxBad   uint32
}
