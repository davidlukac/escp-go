package escp

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"

	"github.com/davidlukac/escp-go/internal/wire"
)

// randReader is the entropy source for session keys; tests replace it to
// provoke the (otherwise unreachable) read failure.
var randReader io.Reader = rand.Reader

// sessionKeyAlphabet is what the characters of a session key are drawn from.
const sessionKeyAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// newSessionKey draws a session key of wire.SessionKeyLen characters from r.
func newSessionKey(r io.Reader) ([]byte, error) {
	key := make([]byte, wire.SessionKeyLen)
	for i := range key {
		n, err := rand.Int(r, big.NewInt(int64(len(sessionKeyAlphabet))))
		if err != nil {
			return nil, err
		}
		key[i] = sessionKeyAlphabet[n.Int64()]
	}
	return key, nil
}

// ensureSession discovers the switch MAC and performs the RSA session-key
// exchange once; the key is dropped on failures so the next call retries.
func (c *Client) ensureSession(ctx context.Context) error {
	if c.session != nil {
		return nil
	}
	if !c.ident.known() {
		if _, err := c.discover(ctx); err != nil {
			return err
		}
	}
	key, err := newSessionKey(randReader)
	if err != nil {
		return err
	}
	c.session = key // the reply is already encrypted with it
	r, err := c.exchange(ctx, wire.OpSet, []wire.TLV{{Type: wire.TLVRSAKey, Value: wire.RSASessionTLV(key)}}, true, []*net.UDPAddr{c.dest})
	if err != nil {
		c.session = nil
		return fmt.Errorf("escp: session key exchange: %w", err)
	}
	if r.err != wire.ErrCodeKeyAccepted {
		c.session = nil
		return fmt.Errorf("escp: session key exchange: %w", &RejectedError{Code: r.err, Reason: "the switch did not accept the session key"})
	}
	c.log.InfoContext(ctx, "session established")
	return nil
}

// withSession runs fn with an established session, re-establishing it once if
// the switch stops answering (session expired or switch rebooted). The one exception is a write
// that fn reports as unconfirmed and that is not repeatable (see repeatable): fn has resent it
// already, so the session is dropped for the next call and the outcome is returned as it is.
func (c *Client) withSession(ctx context.Context, fn func() error) error {
	for retried := false; ; retried = true {
		if err := c.ensureSession(ctx); err != nil {
			return err
		}
		err := fn()
		if errors.Is(err, ErrNoReply) && !retried {
			c.session = nil
			if unconfirmed := (*UnconfirmedWriteError)(nil); errors.As(err, &unconfirmed) && !repeatable(unconfirmed.Method) {
				return err
			}
			c.log.WarnContext(ctx, "no reply; opening a new session", "lookup_by_mac", c.mayRelocate())
			if c.mayRelocate() {
				_ = c.relocate(ctx) // best effort: the address may have changed; the retry says if not
			}
			continue
		}
		return err
	}
}
