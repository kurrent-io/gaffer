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
	Env         string            `json:"env,omitempty" jsonschema:"Environment from gaffer.toml [env.<name>] to subscribe against (live mode only). Omit to use the default environment."`
	Breakpoints []breakpointInput `json:"breakpoints,omitempty" jsonschema:"Source line breakpoints to set before feeding events. Enables debug mode."`
	BreakAt     int64             `json:"break_at,omitempty" jsonschema:"Pause at a specific step (1-based). Enables debug mode."`
}

func (s *Server) handleRun(ctx context.Context, _ *mcp.CallToolRequest, input runInput) (*mcp.CallToolResult, any, error) {
	cfg, root, r := s.requireProject()
	if r != nil {
		return r, nil, nil
	}

	prep := s.prepareRun(cfg, root, input)
	if !prep.block {
		return prep.result, nil, nil
	}

	// Lock-free phase: prepareRun released s.mu so waitForBreak can block.
	wr := s.waitForBreak(ctx, prep.sess, defaultDebugTimeout)

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handleWaitResult(prep.sess, wr)
}

// runPrep is the outcome of the locked pre-block phase. Exactly one of
// the fields is meaningful: result holds a terminal tool response (error
// or the synchronous fixture summary); block==true with a non-nil sess
// means the caller must waitForBreak on sess after the lock is released.
type runPrep struct {
	result *mcp.CallToolResult
	sess   *activeSession
	block  bool
}

// prepareRun runs the entire under-lock phase of a run: create the
// session, set breakpoints, and dispatch to the fixture / live / debug
// path. The single defer s.mu.Unlock() bounds the critical section, so no
// branch can return with the mutex held. The two blocking paths (live and
// fixture-debug) start the background feed, then return block==true so the
// caller waits for a break with the lock released; the synchronous fixture
// path runs to completion under the lock and returns its summary as result.
func (s *Server) prepareRun(cfg *config.Config, root string, input runInput) runPrep {
	s.mu.Lock()
	defer s.mu.Unlock()

	debug := len(input.Breakpoints) > 0 || input.BreakAt > 0

	sess, err := s.createSession(cfg, root, input.Name, debug)
	if err != nil {
		var projErr gafferruntime.ProjectionError
		if errors.As(err, &projErr) {
			// Compile-time projection failure (invalid source,
			// compilation timeout). createSession already fed
			// projection_errors_seen via compileProjection; surface
			// the classified error so the response reflects that the
			// user's code didn't compile.
			return runPrep{result: toolResult(map[string]any{
				"lastError": classifyError(err),
			})}
		}
		return runPrep{result: toolError("%v", err)}
	}

	if debug {
		if err := s.setupBreakpoints(sess, input.Breakpoints); err != nil {
			return runPrep{result: toolError("%v", err)}
		}
	}

	if input.Events == "" {
		if err := s.startLiveMode(sess, input.BreakAt, cfg, root, input.Env); err != nil {
			return runPrep{result: toolError("%v", err)}
		}
		return runPrep{sess: sess, block: true}
	}

	if debug {
		if err := s.runFixtureDebugMode(sess, root, input.Events, input.BreakAt); err != nil {
			return runPrep{result: toolError("%v", err)}
		}
		return runPrep{sess: sess, block: true}
	}

	result, _, _ := s.runFixtureMode(sess, root, input.Events)
	return runPrep{result: result}
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

func (s *Server) startLiveMode(sess *activeSession, breakAt int64, cfg *config.Config, root, envName string) error {
	if breakAt > 0 {
		sess.runner.SetBreakAtStep(breakAt)
	}
	return s.startLiveSubscription(sess, cfg, root, envName)
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

	state, stateErr := sess.runner.CollectState()
	summary := state.ToMap()
	putStateError(summary, stateErr)
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
