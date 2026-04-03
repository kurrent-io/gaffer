package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/subscription"
)

func (s *Server) startLiveSubscription(sess *activeSession) error {
	client, err := s.connectToKurrentDB()
	if err != nil {
		return err
	}

	proj := s.cfg.FindProjection(sess.name)
	engine := config.DefaultEngine
	if proj != nil {
		engine = proj.EffectiveEngine()
	}

	filter := subscription.BuildFilter(sess.info, engine)

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

	if sess.caughtUpCh == nil {
		sess.caughtUpCh = make(chan struct{}, 1)
	}
	if sess.errorCh == nil {
		sess.errorCh = make(chan error, 1)
	}

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
			if sess.caughtUpCh != nil {
				select {
				case sess.caughtUpCh <- struct{}{}:
				default:
				}
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

		s.mu.Lock()
		if ctx.Err() != nil {
			s.mu.Unlock()
			return
		}
		debug := sess.breakCh != nil
		eventCount := sess.stats.Processed + sess.stats.Skipped + sess.stats.Errors + 1
		if debug {
			sess.pausedEvent = eventJSON
			if sess.breakAtPosition > 0 && eventCount == sess.breakAtPosition {
				sess.runtime.Pause()
			}
		}
		s.mu.Unlock()

		// Feed without holding the mutex - allows inspection tools to run
		// while paused at a breakpoint.
		result, feedErr := sess.runtime.Feed(eventJSON)

		s.mu.Lock()
		if ctx.Err() != nil {
			s.mu.Unlock()
			return
		}
		if feedErr != nil {
			sess.stats.Errors++
			sess.stats.Status = "error"
			sess.lastError = feedErr
			_, _ = sess.history.Insert(eventJSON, `{"status":"error"}`)
			s.mu.Unlock()
			if sess.errorCh != nil {
				select {
				case sess.errorCh <- feedErr:
				default:
				}
			}
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
		if debug {
			sess.pausedEvent = ""
		}
		s.mu.Unlock()
	}
}

func classifyError(err error) map[string]any {
	if projErr, ok := err.(gafferruntime.ProjectionError); ok {
		result := map[string]any{
			"code":        projErr.ErrorCode(),
			"description": projErr.ErrorDescription(),
		}
		if hint := errorHint(projErr.ErrorCode()); hint != "" {
			result["hint"] = hint
		}
		return result
	}
	return map[string]any{
		"description": err.Error(),
	}
}

func errorHint(code string) string {
	switch code {
	case "execution-timeout":
		return "Handlers have a 250ms default timeout. Keep handlers fast - do simple state mutations, not heavy computation. See the gotchas resource."
	case "handler-error":
		return "Check that all handlers return state. A missing return makes state undefined on the next call. See the gotchas resource."
	case "state-serialization-error":
		return "State has a 16MB size limit. Aggregate and summarize rather than storing raw event payloads. See the gotchas resource."
	default:
		return ""
	}
}
