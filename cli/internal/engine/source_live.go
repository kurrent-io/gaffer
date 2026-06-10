package engine

import (
	"context"
	"fmt"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/subscription"
)

type LiveSourceConfig struct {
	ConnStr       string
	Root          string
	EnvName       string
	Info          gafferruntime.ProjectionInfo
	EngineVersion int
	OnCaughtUp    func() // called when subscription reaches head of stream, nil = ignore, must not block
	OnFellBehind  func() // called when subscription falls back behind the live tail. nil = ignore, must not block
	// OnConnected fires once, right after Connect returns
	// successfully, with the server's reported major.minor version
	// (or "unknown" when the probe fails). Fires BEFORE
	// subscription.Subscribe, so a Subscribe failure leaves the
	// callback already invoked - the dev wrapper treats that as
	// connected_to_db=true with db_version set, plus a
	// db_disconnect outcome. Used by the dev wrapper to stamp
	// db_version on telemetry. Must not block. nil = ignore.
	OnConnected func(dbVersion string)
}

type liveSource struct {
	cfg LiveSourceConfig
}

func NewLiveSource(cfg LiveSourceConfig) EventSource {
	return &liveSource{cfg: cfg}
}

func (l *liveSource) Run(ctx context.Context, process func(string) bool) error {
	client, err := Connect(l.cfg.ConnStr, l.cfg.Root, l.cfg.EnvName)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if l.cfg.OnConnected != nil {
		l.cfg.OnConnected(ProbeServerVersion(client))
	}

	sub, err := subscription.Subscribe(ctx, client, l.cfg.Info, l.cfg.EngineVersion)
	if err != nil {
		// Subscribe runs after Connect succeeded; a failure here is
		// the connection refusing to subscribe, not an initial
		// connect problem. Tag as disconnect so the outcome
		// distinguishes "couldn't reach the server" from "reached
		// but couldn't keep using it".
		return fmt.Errorf("%w: subscribing: %s", ErrDBDisconnect, err)
	}
	defer func() { _ = sub.Close() }()

	for {
		subEvent := sub.Recv()

		if subEvent.SubscriptionDropped != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("%w: %s", ErrDBDisconnect, subEvent.SubscriptionDropped.Error)
		}

		if subEvent.CaughtUp != nil {
			if l.cfg.OnCaughtUp != nil {
				l.cfg.OnCaughtUp()
			}
			continue
		}

		if subEvent.FellBehind != nil {
			if l.cfg.OnFellBehind != nil {
				l.cfg.OnFellBehind()
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
