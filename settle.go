package escp

import (
	"context"
	"time"
)

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
		select {
		case <-ctx.Done():
			return false
		case <-time.After(poll):
		}
	}
}
