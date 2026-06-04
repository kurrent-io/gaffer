package mcpserver

import (
	"context"
	"errors"
	"fmt"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/pathutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var runTool = &mcp.Tool{
	Name:        "run",
	Description: "Run a projection against events. Creates a new session (replacing any existing one). Always blocks until completion. Fixture mode: returns summary when all events are consumed. Live mode: blocks until caught_up, error, or timeout. Set breakpoints (source lines) or break_at (step number) to pause at specific points - returns debug context with call stack, variables, and state.",
}

type breakpointInput struct {
	Line      int    `json:"line" jsonschema:"Source line number (1-based)"`
	Condition string `json:"condition,omitempty" jsonschema:"JS expression that must be truthy for the breakpoint to fire"`
}

type runInput struct {
	Name        string            `json:"name" jsonschema:"Projection name from gaffer.toml"`
	Events      string            `json:"events,omitempty" jsonschema:"Path to a JSON fixture file (relative to project root or absolute). Omit for live KurrentDB subscription."`
	Breakpoints []breakpointInput `json:"breakpoints,omitempty" jsonschema:"Source line breakpoints to set before feeding events. Enables debug mode."`
	BreakAt     int64             `json:"break_at,omitempty" jsonschema:"Pause at a specific step (1-based). Enables debug mode."`
}

func (s *Server) handleRun(ctx context.Context, _ *mcp.CallToolRequest, input runInput) (*mcp.CallToolResult, any, error) {
	cfg, root, r := s.requireProject()
	if r != nil {
		return r, nil, nil
	}

	s.mu.Lock()

	debug := len(input.Breakpoints) > 0 || input.BreakAt > 0

	sess, err := s.createSession(cfg, root, input.Name, debug)
	if err != nil {
		s.mu.Unlock()
		var projErr gafferruntime.ProjectionError
		if errors.As(err, &projErr) {
			// Compile-time projection failure (invalid source,
			// compilation timeout). Feed projection_errors_seen
			// alongside the tool response so the session's
			// telemetry reflects user code didn't compile.
			s.recordProjectionError(err)
			return toolResult(map[string]any{
				"lastError": classifyError(err),
			}), nil, nil
		}
		return toolError("%v", err), nil, nil
	}

	if debug {
		if err := s.setupBreakpoints(sess, input.Breakpoints); err != nil {
			s.mu.Unlock()
			return toolError("%v", err), nil, nil
		}
	}

	if input.Events == "" {
		if err := s.startLiveMode(sess, input.BreakAt, cfg, root); err != nil {
			s.mu.Unlock()
			return toolError("%v", err), nil, nil
		}
		s.mu.Unlock()
		wr := s.waitForBreak(ctx, sess, defaultDebugTimeout)
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.handleWaitResult(sess, wr)
	}

	if debug {
		if err := s.runFixtureDebugMode(sess, root, input.Events, input.BreakAt); err != nil {
			s.mu.Unlock()
			return toolError("%v", err), nil, nil
		}
		s.mu.Unlock()
		wr := s.waitForBreak(ctx, sess, defaultDebugTimeout)
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.handleWaitResult(sess, wr)
	}

	defer s.mu.Unlock()
	return s.runFixtureMode(sess, root, input.Events)
}

func (s *Server) setupBreakpoints(sess *activeSession, breakpoints []breakpointInput) error {
	bps := make([]engine.Breakpoint, len(breakpoints))
	for i, bp := range breakpoints {
		bps[i] = engine.Breakpoint{
			Line:      bp.Line,
			Condition: bp.Condition,
		}
	}
	snapped, err := sess.runner.SetBreakpoints(bps)
	if err != nil {
		return err
	}
	for i, s := range snapped {
		if s == nil {
			return fmt.Errorf("no breakable step at or after line %d", breakpoints[i].Line)
		}
	}
	return nil
}

func (s *Server) startLiveMode(sess *activeSession, breakAt int64, cfg *config.Config, root string) error {
	if breakAt > 0 {
		sess.runner.SetBreakAtStep(breakAt)
	}
	return s.startLiveSubscription(sess, cfg, root)
}

func (s *Server) runFixtureDebugMode(sess *activeSession, root, eventsPath string, breakAt int64) error {
	eventsPath = pathutil.AnchorUnder(root, eventsPath)

	events, err := engine.LoadEvents(eventsPath)
	if err != nil {
		return err
	}

	if breakAt > int64(len(events)) {
		return fmt.Errorf("break_at %d exceeds total events (%d)", breakAt, len(events))
	}

	sess.runner.SetBreakAtStep(breakAt)

	done := make(chan struct{})
	sess.done = done

	go func() {
		defer close(done)
		source := engine.NewFixtureSource(events)
		_ = source.Run(context.Background(), sess.runner.ProcessOne)

		if !sess.runner.Faulted() {
			sess.runner.SetStatus("completed")
		} else {
			sess.runner.SetStatus("error")
			s.recordProjectionError(sess.runner.LastError())
		}
	}()

	return nil
}

func (s *Server) runFixtureMode(sess *activeSession, root, eventsPath string) (*mcp.CallToolResult, any, error) {
	eventsPath = pathutil.AnchorUnder(root, eventsPath)

	events, err := engine.LoadEvents(eventsPath)
	if err != nil {
		return toolError("%v", err), nil, nil
	}

	source := engine.NewFixtureSource(events)
	_ = source.Run(context.Background(), sess.runner.ProcessOne)

	if !sess.runner.Faulted() {
		sess.runner.SetStatus("completed")
	} else {
		sess.runner.SetStatus("error")
		s.recordProjectionError(sess.runner.LastError())
	}

	summary := sess.runner.CollectState().ToMap()
	summary["completed"] = !sess.runner.Faulted()
	summary["processed"] = sess.handled()
	summary["errors"] = sess.errors()
	summary["totalEvents"] = len(events)

	// Fixture mode: surface skipped count + per-reason breakdown.
	// The user picked the events for this run, so a skip is
	// diagnostic ("you forgot a handler", "partitionBy returned
	// null"). Live mode (the other run paths) keeps it hidden.
	stats := sess.runner.Stats()
	if stats.Skipped > 0 {
		summary["skipped"] = stats.Skipped
		if len(stats.SkippedByReason) > 0 {
			summary["skippedByReason"] = stats.SkippedByReason
		}
	}

	if sess.runner.Faulted() {
		if lastErr := sess.runner.LastError(); lastErr != nil {
			summary["lastError"] = classifyError(lastErr)
		}
	}

	return toolResult(summary), nil, nil
}
