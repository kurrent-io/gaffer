package mcpserver

import (
	"context"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
)

func (s *Server) startLiveSubscription(sess *activeSession, cfg *config.Config, root string) error {
	ctx, cancel := context.WithCancel(context.Background())
	sess.cancel = cancel
	sess.runner.SetStatus("running")

	if sess.caughtUpCh == nil {
		sess.caughtUpCh = make(chan struct{}, 1)
	}
	if sess.errorCh == nil {
		sess.errorCh = make(chan error, 1)
	}
	// sess.done lets closeSession wait for the live goroutine to
	// finish its post-Run bookkeeping (status update +
	// recordProjectionError) before destroying the runner. Without
	// this, the cobra wrapper can drain ProjectionErrors before
	// the goroutine has appended, dropping the telemetry; worse,
	// the goroutine's runner reads could race runner.Destroy().
	done := make(chan struct{})
	sess.done = done

	source := engine.NewLiveSource(engine.LiveSourceConfig{
		ConnStr:       cfg.Connection,
		Root:          root,
		Info:          sess.runner.Info(),
		EngineVersion: sess.runner.EngineVersion(),
		OnCaughtUp: func() {
			if sess.runner.Status() == "running" {
				sess.runner.SetStatus("caught_up")
			}
			select {
			case sess.caughtUpCh <- struct{}{}:
			default:
			}
		},
	})

	go func() {
		defer close(done)
		srcErr := source.Run(ctx, sess.runner.ProcessOne)

		if ctx.Err() != nil {
			sess.runner.SetStatus("stopped")
		} else if sess.runner.Faulted() || srcErr != nil {
			sess.runner.SetStatus("error")
		}

		if sess.runner.Faulted() {
			lastErr := sess.runner.LastError()
			s.recordProjectionError(lastErr)
			if sess.errorCh != nil {
				select {
				case sess.errorCh <- lastErr:
				default:
				}
			}
		}
	}()

	return nil
}

func classifyError(err error) map[string]any {
	fe := engine.ClassifyError(err)
	result := map[string]any{
		"code":        fe.Code,
		"description": fe.Description,
	}
	if hint := errorHint(fe.Code); hint != "" {
		result["hint"] = hint
	}
	return result
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
