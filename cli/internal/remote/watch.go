package remote

import (
	"context"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
)

// WatchDefinition subscribes to a projection's definition stream
// ($projections-<name>) from its current end and calls onUpdate for each
// $ProjectionUpdated event - a deploy, or a lifecycle write (enable / disable /
// abort / reset / config). It blocks until ctx is cancelled or the subscription
// drops, returning the drop error (nil for a clean ctx cancellation) so the
// caller can reconnect.
//
// Subscribing from the end means only writes after this call fire the callback;
// the caller reads current state separately. Subscribing to a stream that
// doesn't exist yet is valid - the projection simply isn't deployed on this env
// yet, and the callback fires when it first is.
func (c *Client) WatchDefinition(ctx context.Context, name string, onUpdate func()) error {
	sub, err := c.db.SubscribeToStream(ctx, projectionStreamPrefix+name, kurrentdb.SubscribeToStreamOptions{
		From: kurrentdb.End{},
	})
	if err != nil {
		return err
	}
	defer sub.Close()

	for {
		ev := sub.Recv()
		switch {
		case ev.EventAppeared != nil:
			if e := ev.EventAppeared.Event; e != nil && e.EventType == projectionUpdatedType {
				onUpdate()
			}
		case ev.SubscriptionDropped != nil:
			// A clean ctx cancellation surfaces as a drop; report it as nil so the
			// caller can tell an intentional teardown from a reconnectable failure.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return ev.SubscriptionDropped.Error
		}
		// CheckPointReached / CaughtUp events carry no definition change - ignore.
	}
}
