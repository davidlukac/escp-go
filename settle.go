package escp

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// plainNoReply is the error of a write that got no reply without its "may have been applied"
// wrapper: a probe settles the outcome (applied, not applied, or ErrMoveUnconfirmed), so what it
// reports must not match ErrWriteUnconfirmed when the state is known.
func plainNoReply(err error) error {
	if ue := (*UnconfirmedWriteError)(nil); errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// cutShort is the error of a call that Close ended while a management write may already have been
// applied (a move or a lease wait, or the settle of a write): the client is closed, and what the
// switch has now is unknown, which is what ErrMoveUnconfirmed says. It matches both.
func cutShort() error {
	return fmt.Errorf("%w: the call was cut short and the switch may have taken the change: %w", ErrClosed, ErrMoveUnconfirmed)
}

// cutShortBy returns err, or cutShort when Close has begun: whatever the wait reported (nothing
// answered, nobody found) was caused by the stop, not by the switch.
func (c *Client) cutShortBy(err error) error {
	if err != nil && c.closing() {
		return cutShort()
	}
	return err
}

// pollUntil asks probe, with a context that ends after wait, until it reports true, pausing poll
// between the asks and calling again (to log, mostly) after each answer that was no. It reports
// whether probe said yes before the time ran out. The wait for a moved switch and the wait for a
// lease are this loop with different probes; what a timeout means is each caller's to say.
func pollUntil(ctx context.Context, wait, poll time.Duration, probe func(context.Context) bool, again func(context.Context)) bool {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		if probe(ctx) {
			return true
		}
		again(ctx)
		pause := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			pause.Stop()
			return false
		case <-pause.C:
		}
	}
}
