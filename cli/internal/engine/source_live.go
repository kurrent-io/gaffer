package engine

import (
	"context"
	"fmt"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/subscription"
)

type liveSource struct {
	connStr string
	root    string
	info    gafferruntime.QuerySources
	version string
}

func NewLiveSource(connStr, root string, info gafferruntime.QuerySources, version string) EventSource {
	return &liveSource{connStr: connStr, root: root, info: info, version: version}
}

func (l *liveSource) Run(ctx context.Context, process func(string) bool) error {
	client, err := Connect(l.connStr, l.root)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	filter := subscription.BuildFilter(l.info, l.version)

	opts := kurrentdb.SubscribeToAllOptions{
		From:           kurrentdb.Start{},
		ResolveLinkTos: subscription.ResolveLinkTos(l.version),
	}
	if filter != nil {
		opts.Filter = filter
	}

	sub, err := client.SubscribeToAll(ctx, opts)
	if err != nil {
		return fmt.Errorf("subscribing: %w", err)
	}
	defer func() { _ = sub.Close() }()

	for {
		subEvent := sub.Recv()

		if subEvent.SubscriptionDropped != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("subscription dropped: %w", subEvent.SubscriptionDropped.Error)
		}

		if subEvent.EventAppeared == nil {
			continue
		}

		eventJSON, err := subscription.MapEvent(subEvent.EventAppeared)
		if err != nil || eventJSON == "" {
			continue
		}

		if process(eventJSON) {
			return nil
		}
	}
}
