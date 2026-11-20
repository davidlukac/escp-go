package escp

import (
	"context"
	"time"

	"github.com/davidlukac/escp-go/model"
)

// The dial options below are what the tests of this package pass to dial (dialOption, client.go). They
// live in a test file because no production code calls them: the production build must not carry
// options that only a test turns (`make deadcode` reports one that does).

// withLoopback lets the client move a switch that lives on 127.0.0.0/8.
func withLoopback() dialOption { return func(c *Client) { c.allowLoopback = true } }

// withClaim makes this client claim its switch even in the tests, which otherwise do not.
func withClaim() dialOption { return func(c *Client) { c.shared = false } }

// withMoveWait shortens the wait for a switch that was told to move.
func withMoveWait(d time.Duration) dialOption { return func(c *Client) { c.moveWait = d } }

// withMovePoll shortens the pause between the probes of that wait.
func withMovePoll(d time.Duration) dialOption { return func(c *Client) { c.movePoll = d } }

// withFirstContact shortens the time the configured address gets to answer the first discovery.
func withFirstContact(d time.Duration) dialOption { return func(c *Client) { c.firstContact = d } }

// withLocator replaces the lookup of a switch by its MAC address.
func withLocator(f func(ctx context.Context, mac model.MAC) (model.Info, error)) dialOption {
	return func(c *Client) { c.locator = f }
}
