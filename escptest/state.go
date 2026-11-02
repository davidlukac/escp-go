package escptest

import (
	"bytes"
	"math/big"

	"github.com/davidlukac/escp-go/internal/wire"
)

// EnableState makes the switch remember settings: every accepted SET stores the value of each
// TLV it carries, and a GET for a type it holds (seeded here or written since) answers with that
// value, so a write can be read back the way the real switch does it. Types it holds nothing
// for fall through to the scripted replies (LED, QoS, port count), and OnGet still wins. The
// seeds are the settings the switch starts with.
func (f *Switch) EnableState(seed ...Setting) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stateful = true
	f.store = map[wire.TLVType][]byte{}
	f.rows = map[wire.TLVType][][]byte{}
	// The real switch answers a request for the IGMP state with report suppression as well.
	f.bundles = map[wire.TLVType][]wire.TLVType{wire.TLVIGMPEnabled: {wire.TLVIGMPSuppression}}
	for _, s := range seed {
		f.store[s.Type] = append([]byte(nil), s.Value...)
	}
}

// Setting is one entry of a request or a reply: a protocol type number (the numbers of
// PROTOCOL.md) and its value. It is the fake's own type, so a test outside the
// escp module needs nothing from the client's internals to read or script traffic.
type Setting struct {
	Type  TLVType
	Value []byte
}

func toTLVs(in []Setting) []wire.TLV {
	out := make([]wire.TLV, len(in))
	for i, s := range in {
		out[i] = wire.TLV{Type: s.Type, Value: s.Value}
	}
	return out
}

func toSettings(in []wire.TLV) []Setting {
	out := make([]Setting, len(in))
	for i, t := range in {
		out[i] = Setting{Type: t.Type, Value: append([]byte(nil), t.Value...)}
	}
	return out
}

// WriteCount is the number of SETs the switch accepted: the answer to "was anything written?".
func (f *Switch) WriteCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.history)
}

// AllSets returns the settings of every accepted SET in order, credentials removed.
func (f *Switch) AllSets() [][]Setting {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]Setting, len(f.history))
	for i, h := range f.history {
		out[i] = toSettings(h)
	}
	return out
}

// LastSet returns the settings of the most recent accepted SET, credentials removed.
func (f *Switch) LastSet() []Setting {
	f.mu.Lock()
	defer f.mu.Unlock()
	return toSettings(f.lastSet)
}

// LED is the LED state the switch holds: the last LED SET it accepted.
func (f *Switch) LED() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.led
}

// SetFirmware changes the firmware version the switch reports in discovery replies.
func (f *Switch) SetFirmware(version string) {
	f.mu.Lock()
	f.firmware = version
	f.mu.Unlock()
}

// Hostname is the device description set by the last hostname SET ("" before any).
func (f *Switch) Hostname() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hostname
}

// IPSent is the IP address carried by the last hostname SET (nil before any).
func (f *Switch) IPSent() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.setIP...)
}

// PortSetting is the 7-byte value of the last port SET addressed to port (nil before any).
func (f *Switch) PortSetting(port int) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.portSets[byte(port)]...)
}

// DecryptSessionKey recovers the session key from the RSA key-exchange value a client sent.
// It returns nil for a value too short to be one.
func DecryptSessionKey(v []byte) []byte {
	if len(v) < 2+2*4 {
		return nil
	}
	words := new(big.Int)
	for i := 3; i >= 0; i-- {
		words.Lsh(words, 16)
		words.Or(words, big.NewInt(int64(v[2+2*i])|int64(v[3+2*i])<<8))
	}
	phi := new(big.Int).Mul(new(big.Int).Sub(fakeP, big.NewInt(1)), new(big.Int).Sub(fakeQ, big.NewInt(1)))
	d := new(big.Int).ModInverse(big.NewInt(wire.RSAExponent), phi)
	m := new(big.Int).Exp(words, d, wire.RSAModulus())
	return wire.ReverseBytes(m.FillBytes(make([]byte, wire.SessionKeyLen)))
}

// EnableRows declares a table the switch keeps one TLV per row of (the ports, the QoS priorities,
// the bandwidth and storm rates) and seeds it. A row is identified by its first byte, the port
// number: a SET replaces the row with the same first byte, a GET returns every row in order.
// A write of the port table (TLV 4096) keeps what only the switch decides, the LAG and the
// actual speed, and derives the actual flow control from the configured one. Call EnableState
// first.
func (f *Switch) EnableRows(typ TLVType, rows ...[]byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[typ] = nil
	for _, r := range rows {
		f.rows[typ] = append(f.rows[typ], append([]byte(nil), r...))
	}
}

// storeSet remembers one written TLV: into its table when it has one, else as a single value.
func (f *Switch) storeSet(t wire.TLV) {
	defer f.applied(t)
	table, ok := f.rows[t.Type]
	if !ok {
		f.store[t.Type] = append([]byte(nil), t.Value...)
		return
	}
	row := append([]byte(nil), t.Value...)
	k := rowKeyLen(t.Type)
	for i, old := range table {
		if !bytes.Equal(old[:k], row[:k]) {
			continue
		}
		if t.Type == wire.TLVPorts && len(row) == len(old) {
			row[portRowLAG], row[portRowActualSpeed] = old[portRowLAG], old[portRowActualSpeed]
			row[portRowActualFlow] = 0
			if row[portRowConfiguredFlow] == 1 && old[portRowActualSpeed] != 0 {
				row[portRowActualFlow] = 1
			}
		}
		table[i] = row
		return
	}
	f.rows[t.Type] = append(table, row)
}

// held returns the stored settings for the requested types, or nil when the switch is not
// stateful or holds none of them.
func (f *Switch) held(req []wire.TLV) []wire.TLV {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.stateful {
		return nil
	}
	var out []wire.TLV
	for _, r := range req {
		for _, extra := range f.bundles[r.Type] {
			out = append(out, f.heldType(extra)...)
		}
		if table, ok := f.rows[r.Type]; ok {
			for _, row := range table {
				out = append(out, wire.TLV{Type: r.Type, Value: append([]byte(nil), row...)})
			}
			continue
		}
		if v, ok := f.store[r.Type]; ok {
			out = append(out, wire.TLV{Type: r.Type, Value: append([]byte(nil), v...)})
		}
	}
	return out
}

// heldType returns the stored TLVs of one type (a table's rows, or the single value).
func (f *Switch) heldType(typ wire.TLVType) []wire.TLV {
	var out []wire.TLV
	if table, ok := f.rows[typ]; ok {
		for _, row := range table {
			out = append(out, wire.TLV{Type: typ, Value: append([]byte(nil), row...)})
		}
	} else if v, ok := f.store[typ]; ok {
		out = append(out, wire.TLV{Type: typ, Value: append([]byte(nil), v...)})
	}
	return out
}

// rowKeyLen is how many leading bytes identify a row of a table: the port number, except for
// the 802.1Q VLANs, which are identified by their 2-byte id.
func rowKeyLen(typ wire.TLVType) int {
	if typ == wire.TLVVLAN8021QVLAN {
		return 2
	}
	return 1
}
