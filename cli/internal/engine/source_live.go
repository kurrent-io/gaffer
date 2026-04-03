package engine

import (
	"context"
	"fmt"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/subscription"
)

type LiveSourceConfig struct {
	ConnStr    string
	Root       string
	Info       gafferruntime.QuerySources
	Version    string
	OnCaughtUp func() // called when subscription reaches head of stream, nil = ignore, must not block
}

type liveSource struct {
	cfg LiveSourceConfig
}

func NewLiveSource(cfg LiveSourceConfig) EventSource {
	return &liveSource{cfg: cfg}
}

func (l *liveSource) Run(ctx context.Context, process func(string) bool) error {
	client, err := Connect(l.cfg.ConnStr, l.cfg.Root)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	filter := subscription.BuildFilter(l.cfg.Info, l.cfg.Version)

	opts := kurrentdb.SubscribeToAllOptions{
		From:           kurrentdb.Start{},
		ResolveLinkTos: subscription.ResolveLinkTos(l.cfg.Version),
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

		if subEvent.CaughtUp != nil {
			if l.cfg.OnCaughtUp != nil {
				l.cfg.OnCaughtUp()
			}
			continue
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
