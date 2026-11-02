package escptest

// SystemSetBehavior is what the switch does after it applies a system SET, the write that
// carries the hostname and the management address.
type SystemSetBehavior int

// The behaviors after a system SET. SystemSetReplies is the well-behaved switch.
const (
	// SystemSetReplies answers the SET and carries on.
	SystemSetReplies SystemSetBehavior = iota
	// SystemSetLeaves applies the SET without answering it, then ignores every packet except
	// discovery, which it still answers: a switch that has moved to its new address, seen from
	// the old one. The fake cannot change its source address, so discovery still reports the
	// new settings from the same address.
	SystemSetLeaves
	// SystemSetVanishes applies the SET without answering it, then ignores everything,
	// discovery included: a switch that moved and cannot be found.
	SystemSetVanishes
)

// Faults is the programmable misbehavior of a Switch. Set it inside Configure, which holds the
// switch's lock; the zero value is a well-behaved switch.
type Faults struct {
	// OnGet scripts the reply to GETs: the settings, the error code, and whether to stay silent.
	OnGet func(types []TLVType) (settings []Setting, code int32, drop bool)
	// RSADrop makes the switch ignore the key exchange.
	RSADrop bool
	// RSACode answers the key exchange with this code instead of the success code.
	RSACode int32
	// DropSets makes the switch ignore authenticated SETs.
	DropSets bool
	// DiscoverReply replaces the discovery reply; nil keeps the default TL-SG108E reply.
	DiscoverReply []Setting
	// RejectSetsFrom rejects the nth and every later authenticated SET (1-based); 0 accepts all.
	RejectSetsFrom int
	// RejectCode is the reply code of a rejected SET; 0 means 9.
	RejectCode int32
	// StaleAfterSet makes the next n discoveries after a system SET still report the old address.
	StaleAfterSet int
	// OnSystemSet is what the switch does after applying a system SET; the default replies.
	OnSystemSet SystemSetBehavior
	// AfterSystemSet, when set, is called (without the lock) once a system SET has been applied
	// and before the switch replies or leaves, so a test can act at exactly that moment.
	AfterSystemSet func()
}

// DropPacket makes the switch ignore the nth packet it receives (1-based), to test retries.
func (f *Switch) DropPacket(n int) { f.mu.Lock(); f.dropNth = n; f.mu.Unlock() }

// DroppedSets is the number of authenticated SET datagrams the switch ignored under DropSets. A
// test of "the write was lost" uses it to show that the write really was sent, so that it does
// not pass for the wrong reason (WriteCount is zero for a write that never left the client too).
func (f *Switch) DroppedSets() int { f.mu.Lock(); defer f.mu.Unlock(); return f.droppedSets }

// Received is the number of datagrams that have reached the switch so far, whatever it did with
// them. A discovery probe is two datagrams (see DropFirst). Datagrams on one socket arrive in
// order, so a count read after a reply to a later request includes every earlier one.
func (f *Switch) Received() int { f.mu.Lock(); defer f.mu.Unlock(); return f.recv }

// DropFirst makes the switch ignore the first n packets it receives. A discovery probe is sent
// twice per attempt (to the switch and to the broadcast address, which a test points at the same
// fake), so DropFirst(2) loses exactly one whole attempt of a probe.
func (f *Switch) DropFirst(n int) { f.mu.Lock(); f.dropFirst = n; f.mu.Unlock() }

// Mute makes the switch ignore everything from now on, as if it had gone away.
func (f *Switch) Mute() { f.mu.Lock(); f.mute = true; f.mu.Unlock() }

// ClearSession forgets the session key, as after a reboot: the client must exchange keys again.
func (f *Switch) ClearSession() { f.mu.Lock(); f.session = nil; f.mu.Unlock() }

// Configure changes the fake's programmable misbehavior (Faults) under its lock.
func (f *Switch) Configure(fn func(*Faults)) { f.mu.Lock(); fn(&f.faults); f.mu.Unlock() }

// defaultRejectCode is the reply code of a SET that Faults.RejectSetsFrom rejects and that names none.
const defaultRejectCode = 9
