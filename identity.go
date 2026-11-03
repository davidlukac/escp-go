package escp

import (
	"net"
	"sync"

	"github.com/davidlukac/escp-go/model"
)

// identity is who a client believes the switch is: the MAC address Config.MAC pins it to or, when
// that names none, the one the switch reported on first contact, and the device that answered in
// place of it, if one did. It has a lock of its own, so that reading it never waits for the
// exchange another call is holding the client's lock for.
type identity struct {
	mu sync.Mutex
	// pin is Config.MAC; zero when the configuration names none.
	pin model.MAC
	// learned is the switch MAC of the first reply this client accepted (have says there was one).
	// It goes into the header of every request.
	learned model.MAC
	have    bool
	// wrong is the device that answered the request in flight with a MAC other than the identity.
	wrong *WrongSwitchError
}

// mac is the MAC address of the switch the client talks to: the pin or, without one, the MAC of the
// first contact. A device that answers with another one is not that switch, whoever took the address
// over. Zero before the first contact of a client that has no pin.
func (i *identity) mac() model.MAC {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.current()
}

// current is mac for a caller that holds i.mu.
func (i *identity) current() model.MAC {
	if !i.pin.IsZero() {
		return i.pin
	}
	if i.have {
		return i.learned
	}
	return model.MAC{}
}

// known reports whether a reply has been accepted yet.
func (i *identity) known() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.have
}

// header is the switch MAC the requests carry: the one learned from the first reply, zero before.
func (i *identity) header() model.MAC {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.learned
}

// judge decides whether a reply that carries switch MAC got and came from ip may be accepted. A
// device whose MAC differs from the identity is refused, and remembered for wrongDevice; so is one
// that sends no MAC at all (all zero) when there is an identity to compare with: a real switch puts
// its MAC in every reply (captured on both of ours, see PROTOCOL.md), and an answer that does not
// say who sent it cannot be told from a device that took the address over. Without an identity yet
// (no pin, no reply so far) the first accepted reply is the first contact: its MAC is learned.
func (i *identity) judge(got model.MAC, from net.IP) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if want := i.current(); !want.IsZero() && got != want {
		i.wrong = &WrongSwitchError{Want: want, Got: got, IP: addrOf(from)}
		return false
	}
	if !i.have {
		i.learned, i.have = got, true
	}
	return true
}

// wrongDevice is the device that was refused since the last forget, nil when none.
func (i *identity) wrongDevice() *WrongSwitchError {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.wrong
}

// forget clears the refused device: a new request starts without a verdict.
func (i *identity) forget() {
	i.mu.Lock()
	i.wrong = nil
	i.mu.Unlock()
}
