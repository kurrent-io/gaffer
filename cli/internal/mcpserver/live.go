package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/subscription"
)

func (s *Server) startLiveSubscription(sess *activeSession) error {
	client, err := s.connectToKurrentDB()
	if err != nil {
		return err
	}

	engine := "v2"
	proj := s.cfg.FindProjection(sess.name)
	if proj != nil && proj.Engine != "" {
		engine = proj.Engine
	}

	filter := subscription.BuildFilter(subscription.SourceInfo{
		AllStreams:                  sess.info.AllStreams,
		Categories:                  sess.info.Categories,
		Streams:                     sess.info.Streams,
		Events:                      sess.info.Events,
		HandlesDeletedNotifications: sess.info.HandlesDeletedNotifications,
	}, engine)

	subOpts := kurrentdb.SubscribeToAllOptions{
		From:           kurrentdb.Start{},
		ResolveLinkTos: subscription.ResolveLinkTos(engine),
	}
	if filter != nil {
		subOpts.Filter = filter
	}

	ctx, cancel := context.WithCancel(context.Background())
	sess.cancel = cancel
	sess.stats.Status = "running"

	sub, err := client.SubscribeToAll(ctx, subOpts)
	if err != nil {
		cancel()
		_ = client.Close()
		return fmt.Errorf("subscribing: %w", err)
	}

	go s.runSubscriptionLoop(ctx, sess, sub, client)

	return nil
}

func (s *Server) runSubscriptionLoop(ctx context.Context, sess *activeSession, sub *kurrentdb.Subscription, client *kurrentdb.Client) {
	defer func() {
		_ = sub.Close()
		_ = client.Close()
	}()

	for {
		subEvent := sub.Recv()

		if subEvent.SubscriptionDropped != nil {
			s.mu.Lock()
			if ctx.Err() != nil {
				sess.stats.Status = "stopped"
			} else {
				sess.stats.Status = "error"
				sess.lastError = subEvent.SubscriptionDropped.Error
			}
			s.mu.Unlock()
			return
		}

		if subEvent.CaughtUp != nil {
			s.mu.Lock()
			if ctx.Err() != nil {
				s.mu.Unlock()
				return
			}
			if sess.stats.Status == "running" {
				sess.stats.Status = "caught_up"
			}
			s.mu.Unlock()
			continue
		}

		if subEvent.EventAppeared == nil {
			continue
		}

		eventJSON, err := subscription.MapEvent(subEvent.EventAppeared)
		if err != nil || eventJSON == "" {
			continue
		}

		s.mu.Lock()
		if ctx.Err() != nil {
			s.mu.Unlock()
			return
		}
		result, feedErr := sess.runtime.Feed(eventJSON)
		if feedErr != nil {
			sess.stats.Errors++
			sess.stats.Status = "error"
			sess.lastError = feedErr
			_, _ = sess.history.Insert(eventJSON, `{"status":"error"}`)
			s.mu.Unlock()
			return
		}

		resultJSON, _ := json.Marshal(result)
		_, _ = sess.history.Insert(eventJSON, string(resultJSON))

		if result.Status == "skipped" {
			sess.stats.Skipped++
		} else {
			sess.stats.Processed++
			if result.Partition != "" {
				sess.partitions[result.Partition] = true
			}
		}
		s.mu.Unlock()
	}
}

func classifyError(err error) map[string]any {
	if projErr, ok := err.(gafferruntime.ProjectionError); ok {
		return map[string]any{
			"code":        projErr.ErrorCode(),
			"description": projErr.ErrorDescription(),
		}
	}
	return map[string]any{
		"description": err.Error(),
	}
}
