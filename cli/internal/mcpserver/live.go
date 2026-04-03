package mcpserver

import (
	"context"
	"fmt"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
)

func (s *Server) startLiveSubscription(sess *activeSession) error {
	ctx, cancel := context.WithCancel(context.Background())
	sess.cancel = cancel
	sess.runner.SetStatus("running")

	if sess.caughtUpCh == nil {
		sess.caughtUpCh = make(chan struct{}, 1)
	}
	if sess.errorCh == nil {
		sess.errorCh = make(chan error, 1)
	}

	debug := sess.breakCh != nil

	if debug {
		return s.startDebugLiveSubscription(ctx, sess)
	}

	proj := s.cfg.FindProjection(sess.name)
	version := config.DefaultEngine
	if proj != nil {
		version = proj.EffectiveEngine()
	}

	sess.runner = engine.NewRunner(engine.RunnerConfig{
		Feed:    engine.FeedFn(sess.runtime.Feed),
		Writer:  nil,
		History: sess.history,
	})

	source := engine.NewLiveSource(engine.LiveSourceConfig{
		ConnStr: s.cfg.Connection,
		Root:    s.root,
		Info:    sess.info,
		Version: version,
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
		srcErr := source.Run(ctx, sess.runner.ProcessOne)

		s.mu.Lock()
		if ctx.Err() != nil {
			sess.runner.SetStatus("stopped")
		} else if sess.runner.Faulted() || srcErr != nil {
			sess.runner.SetStatus("error")
			if srcErr != nil {
				sess.lastError = srcErr
			}
		}
		s.mu.Unlock()

		if sess.runner.Faulted() && sess.errorCh != nil {
			select {
			case sess.errorCh <- fmt.Errorf("projection faulted"):
			default:
			}
		}
	}()

	return nil
}

func (s *Server) startDebugLiveSubscription(ctx context.Context, sess *activeSession) error {
	proj := s.cfg.FindProjection(sess.name)
	version := config.DefaultEngine
	if proj != nil {
		version = proj.EffectiveEngine()
	}

	source := engine.NewLiveSource(engine.LiveSourceConfig{
		ConnStr: s.cfg.Connection,
		Root:    s.root,
		Info:    sess.info,
		Version: version,
		OnCaughtUp: func() {
			sess.runner.SetStatus("caught_up")
			select {
			case sess.caughtUpCh <- struct{}{}:
			default:
			}
		},
	})

	go func() {
		srcErr := source.Run(ctx, sess.runner.ProcessOne)

		if ctx.Err() != nil {
			sess.runner.SetStatus("stopped")
		} else if sess.runner.Faulted() || srcErr != nil {
			sess.runner.SetStatus("error")
		}

		if sess.runner.Faulted() && sess.errorCh != nil {
			select {
			case sess.errorCh <- sess.runner.LastError():
			default:
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
