package escp

import (
	"fmt"
	"net"
	"runtime"
	"strconv"
	"sync"
)

// A switch keeps one session and replies to one fixed UDP port, so two clients of one switch fail
// each other (measured: 30 of 30 attempts). Within one process that can be prevented, and is: a
// client claims the switch it talks to, by its MAC address when it has one and by its address and
// port, and a second client of the same switch is refused when it is made (ErrSwitchInUse). A
// process cannot see another process, so across processes it stays a rule of use.
//
// The registry holds claims, not clients: a client that is dropped without Close is collected, and
// its claim is released then.

var registry = struct {
	mu   sync.Mutex
	held map[string]*claim // key -> holder
}{held: map[string]*claim{}}

// claimsByDefault is whether a new client claims its switch unless told otherwise. It is always
// true in a program; the tests of this package, which put many clients on a handful of fake
// switches and loopback addresses, turn it off in TestMain and ask for claims where they test them.
var claimsByDefault = true

// claim is what one client holds in the registry.
type claim struct {
	keys []string // guarded by registry.mu
}

// claimKeys names the switch by what identifies it: the MAC when configured, the address and port
// when known (a client that finds its switch by MAC has no address before its first request).
func claimKeys(cfg Config, dest *net.UDPAddr) []string {
	var keys []string
	if !cfg.MAC.IsZero() {
		keys = append(keys, "mac "+cfg.MAC.String())
	}
	if dest != nil && dest.IP != nil {
		keys = append(keys, "addr "+net.JoinHostPort(dest.IP.String(), strconv.Itoa(dest.Port)))
	}
	return keys
}

// acquire makes keys the claim's: all of them or none, replacing the keys it held before. It fails
// with ErrSwitchInUse, naming the switch, when another claim holds one of them.
func (cl *claim) acquire(keys []string) error {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for _, k := range keys {
		if holder := registry.held[k]; holder != nil && holder != cl {
			return fmt.Errorf("%w: %s", ErrSwitchInUse, k)
		}
	}
	for _, k := range cl.keys {
		delete(registry.held, k)
	}
	for _, k := range keys {
		registry.held[k] = cl
	}
	cl.keys = keys
	return nil
}

// release gives the claim's keys up; it is idempotent.
func (cl *claim) release() {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	for _, k := range cl.keys {
		delete(registry.held, k)
	}
	cl.keys = nil
}

// held reports whether the claim holds anything.
func (cl *claim) held() bool {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return len(cl.keys) > 0
}

// claimSwitch registers the client's claim on its switch. A client made to share (tests that put
// two clients on one fake) holds none.
func (c *Client) claimSwitch() error {
	if c.shared {
		return nil
	}
	if c.claim == nil {
		c.claim = &claim{}
		runtime.AddCleanup(c, (*claim).release, c.claim)
	}
	return c.claim.acquire(claimKeys(c.cfg, c.dest))
}

// reclaim follows the switch to its new address; a claim another client took meanwhile is not
// stolen, and is logged: the move itself has happened.
func (c *Client) reclaim() {
	if c.shared || c.claim == nil || !c.claim.held() {
		return
	}
	if err := c.claim.acquire(claimKeys(c.cfg, c.dest)); err != nil {
		c.log.Warn("the switch's new address is claimed by another client", "error", err.Error())
	}
}
