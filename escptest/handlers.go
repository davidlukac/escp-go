package escptest

import (
	"github.com/davidlukac/escp-go/internal/wire"
)

// handle answers one packet.
func (f *Switch) handle(r request) {
	f.mu.Lock()
	gone := f.departed && (r.h.Op != wire.OpDiscovery || f.faults.OnSystemSet == SystemSetVanishes)
	f.mu.Unlock()
	if gone {
		return
	}
	switch r.h.Op {
	case wire.OpDiscovery:
		f.handleDiscovery(r)
	case wire.OpSet:
		f.handleSet(r)
	case wire.OpGet:
		f.handleGet(r)
	}
}

// handleSet answers a SET: the configuration read (an authenticated SET with an empty file TLV), the
// key exchange, or a write. Credentials must never travel under the static key, so a SET that carries
// them that way is ignored, as the vendor utility never sends one and a client that did would be
// sending the password in the clear.
func (f *Switch) handleSet(r request) {
	if r.has(wire.TLVUsername) && !r.viaSession {
		return
	}
	switch {
	case r.has(wire.TLVConfigFile) && len(r.tlvs) == 3:
		f.handleConfigRead(r)
	case r.has(wire.TLVRSAKey):
		f.handleKeyExchange(r)
	default:
		f.handleWrite(r)
	}
}

// credentialsOK reports whether the request carries the user name and password the switch accepts.
func (f *Switch) credentialsOK(r request) bool {
	u, _ := r.find(wire.TLVUsername)
	p, _ := r.find(wire.TLVPassword)
	return wire.CString(u) == f.user && wire.CString(p) == f.password
}

// replyBadCredentials answers a SET whose user name or password is wrong (code 5).
func (f *Switch) replyBadCredentials(r request) { f.reply(r, wire.OpSetReply, 5, nil, true) }

// handleKeyExchange takes the session key, or refuses or ignores it as the test asked.
func (f *Switch) handleKeyExchange(r request) {
	v, _ := r.find(wire.TLVRSAKey)
	f.mu.Lock()
	drop, code := f.faults.RSADrop, f.faults.RSACode
	if key := DecryptSessionKey(v); !drop && key != nil {
		f.session = key
	}
	f.mu.Unlock()
	if drop {
		return
	}
	if code == 0 {
		code = wire.ErrCodeKeyAccepted
	}
	f.reply(r, wire.OpSetReply, code, nil, true)
}

// handleWrite answers a SET that changes a setting: it may be ignored, refused for its credentials,
// rejected on purpose or refused by the model; otherwise it is recorded and applied.
func (f *Switch) handleWrite(r request) {
	f.mu.Lock()
	dropSets, rejectFrom, rejectCode, accepted := f.faults.DropSets, f.faults.RejectSetsFrom, f.faults.RejectCode, len(f.history)
	if dropSets {
		f.droppedSets++
	}
	f.mu.Unlock()
	if dropSets {
		return
	}
	if !f.credentialsOK(r) {
		f.replyBadCredentials(r)
		return
	}
	if rejectFrom > 0 && accepted+1 >= rejectFrom {
		if rejectCode == 0 {
			rejectCode = defaultRejectCode
		}
		f.reply(r, wire.OpSetReply, rejectCode, nil, true)
		return
	}
	if code := f.refuse(r.tlvs); code != 0 {
		f.reply(r, wire.OpSetReply, code, nil, true)
		return
	}
	f.record(r)
	f.applyPortSetting(r)
	f.applySystem(r)
	if v, ok := r.find(wire.TLVLED); ok && len(v) == 1 {
		f.mu.Lock()
		f.led = v[0] == 1
		f.mu.Unlock()
	}
	if f.leavesAfter(r) {
		return
	}
	f.reply(r, wire.OpSetReply, 0, nil, true)
}

// record remembers an accepted write, without the credentials, and stores it when the switch keeps state.
func (f *Switch) record(r request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastSet = nil
	for _, t := range r.tlvs {
		if t.Type != wire.TLVUsername && t.Type != wire.TLVPassword {
			f.lastSet = append(f.lastSet, wire.TLV{Type: t.Type, Value: append([]byte(nil), t.Value...)})
		}
	}
	f.history = append(f.history, append([]wire.TLV(nil), f.lastSet...))
	if f.stateful {
		for _, t := range f.lastSet {
			f.storeSet(t)
		}
	}
}

// portEntryLen is the length of a port setting entry (TLV 4096).
const portEntryLen = 7

// applyPortSetting remembers the last port entry written for its port.
func (f *Switch) applyPortSetting(r request) {
	v, ok := r.find(wire.TLVPorts)
	if !ok || len(v) != portEntryLen {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.portSets == nil {
		f.portSets = map[byte][]byte{}
	}
	f.portSets[v[0]] = append([]byte(nil), v...)
}

// applySystem applies the device description and the management address of a system SET, with the
// DHCP behavior measured on a real switch.
func (f *Switch) applySystem(r request) {
	name, ok := r.find(wire.TLVHostname)
	if !ok {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hostname = wire.CString(name)
	f.setIP, _ = r.find(wire.TLVIP)
	f.staleDiscoveries = f.faults.StaleAfterSet
	if v, ok := r.find(wire.TLVDHCP); ok && len(v) == 1 {
		if v[0] != 0 && !f.dhcp {
			f.acquiring = f.lease.Silence // asking for a lease: nothing answers for a moment
		}
		f.dhcp = v[0] != 0
	}
	// A switch that leases its address ignores the static one in the request (measured).
	if ip, ok := r.find(wire.TLVIP); ok && !f.dhcp {
		mask, _ := r.find(wire.TLVNetmask)
		gw, _ := r.find(wire.TLVGateway)
		f.addr = map[wire.TLVType][]byte{wire.TLVIP: ip, wire.TLVNetmask: mask, wire.TLVGateway: gw}
	}
}

// leavesAfter runs the test's hook once a system SET has been applied and reports whether the switch
// leaves without answering it (Faults.OnSystemSet). A SET that is not a system SET never leaves.
func (f *Switch) leavesAfter(r request) bool {
	if !r.has(wire.TLVHostname) {
		return false
	}
	f.mu.Lock()
	leaves := f.faults.OnSystemSet != SystemSetReplies
	f.departed = f.departed || leaves
	applied := f.faults.AfterSystemSet
	f.mu.Unlock()
	if applied != nil {
		applied()
	}
	return leaves
}

// handleGet answers a GET: the test's script first, then what the switch holds, then the fixed
// replies of a fresh switch.
func (f *Switch) handleGet(r request) {
	f.mu.Lock()
	scripted := f.faults.OnGet
	f.mu.Unlock()
	if scripted != nil {
		types := make([]wire.TLVType, len(r.tlvs))
		for i, t := range r.tlvs {
			types[i] = t.Type
		}
		settings, code, drop := scripted(types)
		if !drop {
			f.reply(r, wire.OpReply, code, toTLVs(settings), true)
		}
		return
	}
	if held := f.held(r.tlvs); len(held) > 0 {
		f.reply(r, wire.OpReply, 0, held, true)
		return
	}
	if fresh, ok := f.freshGet(r); ok {
		f.reply(r, wire.OpReply, 0, fresh, true)
	}
}

// freshGet is the reply of a freshly reset switch to the first type of the request that it knows,
// for the settings that nothing has written and no test has seeded.
func (f *Switch) freshGet(r request) ([]wire.TLV, bool) {
	switch {
	case r.has(wire.TLVQoSMode):
		return []wire.TLV{{Type: wire.TLVQoSMode, Value: []byte{0}}}, true
	case r.has(wire.TLVQoSPriority):
		var rows []wire.TLV
		for p := 1; p <= 8; p++ {
			rows = append(rows, wire.TLV{Type: wire.TLVQoSPriority, Value: []byte{byte(p), 0}})
		}
		return rows, true
	case r.has(wire.TLVPortCount):
		return []wire.TLV{{Type: wire.TLVPortCount, Value: []byte{8}}}, true
	case r.has(wire.TLVVLANPortBased):
		// the port-based VLAN state of a fresh switch: mode on, VLAN 1 holding every port, up to 8 groups
		return []wire.TLV{
			{Type: wire.TLVVLANPortBased, Value: []byte{1}},
			{Type: wire.TLVVLANPortBasedMax, Value: []byte{8}},
			{Type: wire.TLVVLANPortBasedVID, Value: []byte{1, 0, 0, 0, 0xff}},
		}, true
	case r.has(wire.TLVAutoSave):
		return []wire.TLV{{Type: wire.TLVAutoSave, Value: []byte{1}}}, true
	case r.has(wire.TLVLED):
		f.mu.Lock()
		v := byte(0)
		if f.led {
			v = 1
		}
		f.mu.Unlock()
		return []wire.TLV{{Type: wire.TLVLED, Value: []byte{v}}}, true
	}
	return nil, false
}

// Offsets in a 7-byte port row (TLV 4096).
const (
	portRowLAG            = 2
	portRowActualSpeed    = 4
	portRowConfiguredFlow = 5
	portRowActualFlow     = 6
)
