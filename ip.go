package escp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/davidlukac/escp-go/model"
)

const (
	// ipMoveWait is how long SetIPConfig waits for the switch to answer at its new address.
	ipMoveWait = 20 * time.Second
	// settleBudget bounds the probe after a write whose outcome is unknown when the caller's context
	// is already done (canceled or expired): the probe must still run, but not for ever.
	settleBudget = 15 * time.Second
	// ipMovePoll is the pause between discovery probes while waiting.
	ipMovePoll = 500 * time.Millisecond
)

// ErrMoveUnconfirmed means the switch was told to move but it is not known whether it did:
// it did not answer at the new address in time, or it did not answer the write and a probe
// found nothing, or something else answered. Its address is then unknown: look at both.
var ErrMoveUnconfirmed = errors.New("escp: the switch did not answer at its new address")

// ipConfigFromInfo parses the address triple of a discovery reply. It refuses
// a reply that does not carry three IPv4 addresses, because anything written
// back from it could cut the management path.
func ipConfigFromInfo(info model.Info) (model.IPConfig, error) {
	cfg := model.IPConfig{IP: info.IP, Netmask: info.Netmask, Gateway: info.Gateway}
	if !cfg.IP.Is4() || !cfg.Netmask.Is4() || !cfg.Gateway.Is4() {
		return model.IPConfig{}, fmt.Errorf("escp: switch reported an unusable IP configuration (%q %q %q); refusing to write", info.IP, info.Netmask, info.Gateway)
	}
	return cfg, nil
}

// addrOf converts an address of the net package; nil and anything that is no address give the
// invalid Addr, and an IPv4 address in its IPv6 form is the IPv4 address.
func addrOf(ip net.IP) netip.Addr {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.Addr{}
	}
	return a.Unmap()
}

// SetIPConfig gives the switch a static management address, keeping the device description as
// the switch reports it. It turns DHCP off: a switch that leases its address ignores a static
// one written next to the flag (measured on firmware 20250710), so the flag is
// written off with the address, and the switch is at that address a fraction of a second later.
//
// DANGER: this moves the switch. The request is answered from the old or the
// new address, so the client accepts replies from both and, once the switch
// confirms, follows it: later calls on this client go to the new address.
//
// A switch that applies the write may never answer it from the old address, so
// when the write gets no reply the client probes both addresses and compares
// what answers with what was asked and with what was there before (by MAC
// address, so a different device does not count). The outcomes: the switch
// has the new settings, and the call succeeds as if the reply had arrived; the
// switch is unchanged after all the attempts went unanswered, and the error says the
// write was not applied (it matches ErrNoReply); or nothing can be established, and the
// error matches ErrMoveUnconfirmed. In that last case the state is unknown and the caller
// must look at both addresses before it believes either. The same holds when ctx is
// canceled or expires after the write was sent: the probe still runs (for a bounded
// time), because "canceled" would not say whether the switch moved; a probe that still
// shows the old settings then proves little (the write may be taking effect), so it is
// reported as ErrMoveUnconfirmed, not as "not applied". A write the switch refuses, or the gate refuses, is not probed:
// nothing was applied.
//
// Callers must guard: a wrong address, mask or gateway can cut the management path for good.
// EnableDHCP is the way back to a lease.
func (c *Client) SetIPConfig(ctx context.Context, cfg model.IPConfig) error {
	if err := c.validateIPConfig(cfg); err != nil {
		return err
	}
	end, err := c.beginModify(ctx)
	if err != nil {
		return err
	}
	defer end()
	info, err := c.Info(ctx)
	if err != nil {
		return err
	}
	before, err := ipConfigFromInfo(info)
	if err != nil {
		return err
	}
	// Taken without the context: Info has just returned, nothing has been sent, and this is a few
	// instructions under the lock (a caller that gave up is refused by the write that follows).
	c.mu.Lock()
	c.alsoFrom = net.IP(cfg.IP.AsSlice())
	c.mu.Unlock()
	sent, err := c.writeSystem(ctx, model.MethodSetIPConfig.Operation(), info.Hostname, false, cfg)
	c.mu.Lock()
	defer c.mu.Unlock()
	var refused *RejectedError
	if err != nil && sent && !errors.As(err, &refused) {
		// The write may have been applied: no reply came, or the call was canceled, or the
		// request could not be sent in full. Only a refusal by the switch (or by the gate, which
		// sends nothing) says it was not. alsoFrom is still set, so a probe is answered from
		// either address; and the probe must run even if ctx is done, because the answer is what
		// the caller needs most.
		if ctx.Err() != nil {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), settleBudget)
			defer cancel()
		}
		err = c.settleLostReply(ctx, info.MAC, before, cfg, err)
	}
	c.alsoFrom = nil
	if err != nil {
		return err
	}
	c.dest = &net.UDPAddr{IP: net.IP(cfg.IP.AsSlice()), Port: c.dest.Port}
	c.session = nil
	if err := c.reclaim(); err != nil {
		return fmt.Errorf("the switch has moved to %s: %w", cfg.IP, err)
	}
	return c.awaitAddress(ctx, cfg)
}

// lostReplyOutcome is what a probe shows after a write that got no reply.
type lostReplyOutcome int

const (
	// lostReplyApplied: the switch has the settings that were asked for.
	lostReplyApplied lostReplyOutcome = iota + 1
	// lostReplyUnchanged: the switch still has the settings it had before.
	lostReplyUnchanged
	// lostReplyOtherDevice: a different device answered.
	lostReplyOtherDevice
	// lostReplyUnexpected: the switch answered with settings that are neither.
	lostReplyUnexpected
)

// classifyLostReply compares what answered a probe after an unanswered write with the
// switch it was sent to (its MAC address) and with the settings before and wanted. When
// the wanted settings equal the old ones the switch is where it was asked to be either way.
func classifyLostReply(mac model.MAC, before, want model.IPConfig, after model.Info) lostReplyOutcome {
	if after.MAC != mac {
		return lostReplyOtherDevice
	}
	got, err := ipConfigFromInfo(after)
	switch {
	case err != nil:
		return lostReplyUnexpected
	case got == want && !after.DHCP:
		return lostReplyApplied
	case got == before:
		return lostReplyUnchanged
	default:
		return lostReplyUnexpected
	}
}

// settleLostReply is called when the write got no reply. It returns nil when a probe shows
// that the switch has the new settings, and otherwise an error that says what is known.
// Callers hold c.mu.
func (c *Client) settleLostReply(ctx context.Context, mac model.MAC, before, want model.IPConfig, lost error) error {
	c.log.WarnContext(ctx, "the write got no reply; asking the switch what it has", "error", lost.Error())
	after, err := c.discover(ctx)
	if err != nil {
		return fmt.Errorf("%w: the write got no reply and a probe found nothing at %s or %s", ErrMoveUnconfirmed, before.IP, want.IP)
	}
	switch classifyLostReply(mac, before, want, after) {
	case lostReplyApplied:
		return nil
	case lostReplyUnchanged:
		if !errors.Is(lost, ErrNoReply) {
			// The call ended (canceled, expired, or a send failed) possibly moments after the
			// write went out, before the switch had to answer it: "unchanged" is only how the
			// switch looked at the probe, and the write may still take effect.
			return fmt.Errorf("%w: %w; a probe still shows the switch unchanged at %s, but the write may not have taken effect yet", ErrMoveUnconfirmed, lost, before.IP)
		}
		return fmt.Errorf("%w; a probe shows the switch unchanged at %s, so the write was not applied", lost, before.IP)
	case lostReplyOtherDevice:
		return fmt.Errorf("%w: the write got no reply and a different device (MAC %s) answers a probe", ErrMoveUnconfirmed, after.MAC)
	default:
		return fmt.Errorf("%w: the write got no reply and the switch now reports ip %s mask %s gateway %s, which is neither the old nor the new settings", ErrMoveUnconfirmed, after.IP, after.Netmask, after.Gateway)
	}
}

// pollInterval is the pause between the probes of a wait for a moved switch or a lease.
func (c *Client) pollInterval() time.Duration {
	if c.movePoll > 0 {
		return c.movePoll
	}
	return ipMovePoll
}

// awaitAddress probes the new address until the switch reports it, because
// the first packets can fail while the neighbor caches of the network catch up
// (for example "host is down" from an earlier unanswered ARP). Callers hold c.mu.
func (c *Client) awaitAddress(ctx context.Context, want model.IPConfig) error {
	wait := c.moveWait
	if wait == 0 {
		wait = ipMoveWait
	}
	moved := pollUntil(ctx, wait, c.pollInterval(),
		func(ctx context.Context) bool {
			info, err := c.discover(ctx)
			return err == nil && info.IP == want.IP && !info.DHCP
		},
		func(ctx context.Context) {
			c.log.DebugContext(ctx, "the switch is not at the new address yet; asking again", "want", want.IP.String(), "poll", c.pollInterval().String())
		})
	if !moved {
		return fmt.Errorf("%w (%s)", ErrMoveUnconfirmed, want.IP)
	}
	return nil
}

// validateIPConfig is IPConfig.Validate, or ValidateAllowingLoopback when a test allows the loopback.
func (c *Client) validateIPConfig(cfg model.IPConfig) error {
	if c.allowLoopback {
		return cfg.ValidateAllowingLoopback()
	}
	return cfg.Validate()
}
