package escp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/davidlukac/escp-go/internal/wire"
	"github.com/davidlukac/escp-go/model"
)

// lock takes the client's lock, which is held across a whole exchange (the switch answers one
// request at a time), and gives up when ctx ends first: a call that queues behind another one must
// not outlive its own context. It returns ctx's error, or ErrClosed for a closed client, with nothing
// held, or nil with the lock held.
// The calls that must run to the end whatever their context says (settling the outcome of a write
// that may have been applied) take c.mu directly.
func (c *Client) lock(ctx context.Context) error {
	if c.stop == nil { // the zero Client: not made by Dial, closed
		return ErrClosed
	}
	if err := acquire(ctx, &c.mu); err != nil {
		return err
	}
	if c.closed || c.closing() {
		c.mu.Unlock()
		return ErrClosed
	}
	return nil
}

// closing reports whether Close has begun, which is before it holds the lock.
func (c *Client) closing() bool {
	select {
	case <-c.stop:
		return true
	default:
		return false
	}
}

// withStop returns ctx ended also when Close begins, and the function that releases it. An exchange
// uses the result for everything it waits on, so that Close does not wait for a silent switch.
func (c *Client) withStop(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(ctx)
	go func() {
		select {
		case <-c.stop:
			cancel(ErrClosed)
		case <-ctx.Done():
		}
	}()
	return ctx, func() { cancel(context.Canceled) }
}

// closedBy turns the cancellation that withStop caused into ErrClosed, and leaves any other error
// as it is.
func closedBy(ctx context.Context, err error) error {
	if err != nil && errors.Is(context.Cause(ctx), ErrClosed) {
		return ErrClosed
	}
	return err
}

// acquire takes mu unless ctx ends first. It returns ctx's error with nothing held, or nil with mu
// held; a lock that arrives after ctx ended is handed straight back.
func acquire(ctx context.Context, mu *sync.Mutex) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	got := make(chan struct{})
	go func() {
		mu.Lock()
		close(got)
	}()
	select {
	case <-got:
		return nil
	case <-ctx.Done():
		go func() { // the lock arrives later and is handed straight back
			<-got
			mu.Unlock()
		}()
		return ctx.Err()
	}
}

// beginModify starts a call that reads a setting, changes part of it and writes all of it back:
// SetHostname, SetIPConfig and EnableDHCP (the System page) and SetPortPriorities (every port's
// priority). Two such calls at once would each write back what they read, and the later write
// would undo the change of the earlier one. c.mu cannot be held from the read to the write, because
// the write gate reads through the client, so these calls hold c.modify instead. It returns ctx's
// error with nothing held, or the function that ends the call; it is taken before c.mu, never
// while holding it.
func (c *Client) beginModify(ctx context.Context) (end func(), err error) {
	if c.stop == nil {
		return nil, ErrClosed
	}
	if err := acquire(ctx, &c.modify); err != nil {
		return nil, err
	}
	return c.modify.Unlock, nil
}

func boolByte(b bool) byte {
	if b {
		return 1
	}
	return 0
}

// setTLVs sends a SET carrying the credentials plus the given settings. It is
// the only place where the client changes the switch, so the write gate sits
// here: no setting can be added without passing it. (The two other SETs are the
// session key exchange and ConfigBackup, which only reads; a test pins every
// place that sends an OpSet.) The gate runs before the lock is
// taken because it reads through the client. op describes the write to the gate
// (the gate gets a copy of its ports).
func (c *Client) setTLVs(ctx context.Context, op model.Operation, tlvs ...wire.TLV) error {
	_, err := c.setTLVsSent(ctx, op, tlvs...)
	return err
}

// setTLVsSent is setTLVs that also reports whether the gate let the write go, so that the
// request may have reached the switch: false means nothing was sent for certain.
func (c *Client) setTLVsSent(ctx context.Context, op model.Operation, tlvs ...wire.TLV) (sent bool, err error) {
	if gate := c.cfg.WriteGate; gate != nil {
		if err := gate(ctx, c, op); err != nil {
			c.log.DebugContext(ctx, "write refused by the gate", "operation", op.Name.String(), "impact", op.Impact.String())
			return false, err
		}
	}
	if err := c.lock(ctx); err != nil {
		return true, err // the gate let it go: sent reports that, as it always has, and a caller that settles an unknown outcome stays on the safe side
	}
	defer c.mu.Unlock()
	c.log.InfoContext(ctx, "write", "operation", op.Name.String(), "impact", op.Impact.String(), "ports", op.Ports.String())
	return true, c.withSession(ctx, func() error {
		req := append([]wire.TLV{
			{Type: wire.TLVUsername, Value: append([]byte(c.cfg.Username), 0)},
			{Type: wire.TLVPassword, Value: append([]byte(c.cfg.Password.Reveal()), 0)},
		}, tlvs...)
		r, err := c.exchange(ctx, wire.OpSet, req, false, []*net.UDPAddr{c.dest})
		if errors.Is(err, ErrNoReply) {
			return &UnconfirmedWriteError{Method: op.Name, Err: err}
		}
		if err != nil {
			return err
		}
		if r.err != 0 {
			return &RejectedError{Code: r.err}
		}
		return nil
	})
}

// repeatable reports whether a write may be run a second time after a new session key. Only the
// writes that cannot cut the management path are: they set absolute values, so a repeat is
// harmless. A management write may depend on the state its first send changed (a VLAN it deleted,
// the address it moved), so it is not repeated: the caller learns that the outcome is unknown.
func repeatable(m model.Method) bool {
	switch m.Operation().Impact {
	case model.ImpactCosmetic, model.ImpactTraffic:
		return true
	case model.ImpactManagement:
		return false
	}
	return false // a method outside the table has no class: fail closed
}

// getTLVs reads one or more setting types with an authenticated session and
// returns every TLV of the reply. It never writes.
func (c *Client) getTLVs(ctx context.Context, types ...wire.TLVType) ([]wire.TLV, error) {
	if err := c.lock(ctx); err != nil {
		return nil, err
	}
	defer c.mu.Unlock()
	var out []wire.TLV
	err := c.withSession(ctx, func() error {
		req := make([]wire.TLV, len(types))
		for i, t := range types {
			req[i] = wire.TLV{Type: t}
		}
		r, err := c.exchange(ctx, wire.OpGet, req, false, []*net.UDPAddr{c.dest})
		if err != nil {
			return err
		}
		if r.err != 0 {
			return &RejectedError{Code: r.err, Reason: fmt.Sprintf("the read of TLV types %v was refused", types)}
		}
		out = r.tlvs
		return nil
	})
	return out, err
}

// requirePorts refuses ports the switch does not have. A PortSet only holds ports that fit
// the 32-bit port mask; the switch may have far fewer (8 on a TL-SG108E), and
// what it does with a write for a port beyond them is unknown. The port count is read from
// the switch once and kept, so the first write that names a port costs one extra read; if
// that read fails, so does the write, with that error. It reads only, and so is not gated.
func (c *Client) requirePorts(ctx context.Context, ports model.PortSet) error {
	if ports.IsEmpty() {
		return nil
	}
	have, err := c.portCount(ctx)
	if err != nil {
		return err
	}
	last := model.PortID(have) // the ports are numbered 1 to the count
	for _, p := range ports.Ports() {
		if p > last {
			return invalidf("escp: port %d does not exist: this switch has %d ports", p, have)
		}
	}
	return nil
}

// portCount returns the switch's number of ports, reading it on first use.
func (c *Client) portCount(ctx context.Context) (int, error) {
	if known := int(c.switchPorts.Load()); known > 0 {
		return known, nil
	}
	tlvs, err := c.getTLVs(ctx, wire.TLVPortCount)
	if err != nil {
		return 0, err
	}
	for _, t := range tlvs {
		if t.Type == wire.TLVPortCount && len(t.Value) == 1 && t.Value[0] > 0 {
			c.switchPorts.Store(int64(t.Value[0]))
			return int(t.Value[0]), nil
		}
	}
	return 0, errors.New("escp: the switch did not report its number of ports")
}
