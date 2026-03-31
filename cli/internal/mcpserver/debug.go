package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/projection"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var debugTool = &mcp.Tool{
	Name:        "debug",
	Description: "Run a projection with debug enabled, pausing at a specific event. Creates a new session (replacing any existing one). Set break_at to choose which event to debug. Optionally set line to break at a specific source line instead of pausing at handler entry. Session stays paused for evaluate/step/continue calls.",
}

type debugInput struct {
	Name      string `json:"name" jsonschema:"Projection name from gaffer.toml"`
	Events    string `json:"events" jsonschema:"Path to a JSON fixture file (relative to project root or absolute)"`
	BreakAt   int64  `json:"break_at" jsonschema:"Event position (1-based) to pause at"`
	Line      int    `json:"line,omitempty" jsonschema:"Source line to set a breakpoint on (instead of pausing at entry). 1-based."`
	Condition string `json:"condition,omitempty" jsonschema:"JS expression that must be truthy for the breakpoint to fire (e.g. 'state.count > 5'). Requires line to be set."`
}

func (s *Server) handleDebug(_ context.Context, _ *mcp.CallToolRequest, input debugInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if input.Events == "" {
		return toolError("events path is required for debug"), nil, nil
	}
	if input.BreakAt < 1 {
		return toolError("break_at must be >= 1"), nil, nil
	}

	// Create a debug-enabled session
	sess, err := s.createSession(input.Name, true)
	if err != nil {
		if _, ok := err.(gafferruntime.ProjectionError); ok {
			return toolResult(map[string]any{
				"lastError": classifyError(err),
			}), nil, nil
		}
		return toolError("%v", err), nil, nil
	}

	eventsPath := input.Events
	if !filepath.IsAbs(eventsPath) {
		eventsPath = filepath.Join(s.root, eventsPath)
	}

	events, err := projection.LoadEvents(eventsPath)
	if err != nil {
		return toolError("%v", err), nil, nil
	}

	if int(input.BreakAt) > len(events) {
		return toolError("break_at %d exceeds total events (%d)", input.BreakAt, len(events)), nil, nil
	}

	// Set up break signaling - channel reused across steps
	breakCh := make(chan gafferruntime.BreakInfo, 1)
	sess.breakCh = breakCh
	sess.runtime.OnBreak(func(info gafferruntime.BreakInfo) {
		breakCh <- info
	})

	// Feed events up to the target without debugging
	for i := int64(0); i < input.BreakAt-1; i++ {
		result, feedErr := sess.runtime.Feed(events[i])
		if feedErr != nil {
			return toolError("error at event %d: %v", i+1, feedErr), nil, nil
		}
		resultJSON, _ := json.Marshal(result)
		_, _ = sess.history.Insert(events[i], string(resultJSON))
		s.recordResult(sess, result)
	}

	if input.Condition != "" && input.Line == 0 {
		return toolError("condition requires line to be set"), nil, nil
	}

	// Set breakpoint: either a source line or pause at entry
	if input.Line > 0 {
		var opts *gafferruntime.BreakpointOptions
		if input.Condition != "" {
			opts = &gafferruntime.BreakpointOptions{Condition: input.Condition}
		}
		snapped, err := sess.runtime.SetBreakpoint(input.Line, 0, opts)
		if err != nil {
			return toolError("setting breakpoint: %v", err), nil, nil
		}
		if snapped == nil {
			return toolError("no breakable position at or after line %d", input.Line), nil, nil
		}
	} else {
		sess.runtime.Pause()
	}

	// Feed the target event in a goroutine (it will block at the breakpoint)
	targetEvent := events[input.BreakAt-1]
	feedDone := make(chan feedOutcome, 1)
	go func() {
		result, err := sess.runtime.Feed(targetEvent)
		feedDone <- feedOutcome{result: result, err: err}
	}()

	// Wait for break or feed completion (event may be skipped/errored without pausing)
	select {
	case breakInfo := <-breakCh:
		debugContext := s.collectDebugContext(sess, breakInfo)

		// Leave the session paused for evaluate/continue calls
		sess.paused = true
		sess.feedDone = feedDone
		sess.pausedEvent = targetEvent
		sess.stats.Status = "paused"

		debugContext["position"] = input.BreakAt
		debugContext["totalEvents"] = len(events)
		debugContext["paused"] = true
		debugContext["hint"] = "Session is paused. Use 'evaluate' to inspect expressions, then 'debug_continue' to resume."
		return toolResult(debugContext), nil, nil

	case outcome := <-feedDone:
		// Feed completed without breaking - event was skipped or errored
		result := map[string]any{
			"position":    input.BreakAt,
			"totalEvents": len(events),
			"paused":      false,
		}

		if outcome.err != nil {
			result["feedError"] = classifyError(outcome.err)
			_, _ = sess.history.Insert(targetEvent, `{"status":"error"}`)
			sess.stats.Status = "error"
		} else {
			resultJSON, _ := json.Marshal(outcome.result)
			_, _ = sess.history.Insert(targetEvent, string(resultJSON))
			s.recordResult(sess, outcome.result)
			sess.stats.Status = "completed"
			result["note"] = fmt.Sprintf("event at position %d was %s - no breakpoint hit", input.BreakAt, outcome.result.Status)
			if outcome.result.Status == "skipped" {
				result["skipReason"] = outcome.result.SkipReason
			}
		}

		return toolResult(result), nil, nil
	}
}

var evaluateTool = &mcp.Tool{
	Name:        "evaluate",
	Description: "Evaluate a JavaScript expression in the current debug context. Only works while paused at a breakpoint (after calling debug). Returns the expression result with type information.",
}

type evaluateInput struct {
	Expression string `json:"expression" jsonschema:"JavaScript expression to evaluate in the current scope"`
}

func (s *Server) handleEvaluate(_ context.Context, _ *mcp.CallToolRequest, input evaluateInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.session == nil {
		return toolError("no active session - call debug first"), nil, nil
	}
	if !s.session.paused {
		return toolError("session is not paused - call debug with break_at first"), nil, nil
	}
	if input.Expression == "" {
		return toolError("expression is required"), nil, nil
	}

	variable, err := s.session.runtime.Evaluate(input.Expression)
	if err != nil {
		return toolError("evaluate failed: %v", err), nil, nil
	}

	return toolResult(map[string]any{
		"expression": input.Expression,
		"value":      variable.Value,
		"type":       variable.Type,
	}), nil, nil
}

var debugContinueTool = &mcp.Tool{
	Name:        "debug_continue",
	Description: "Resume execution after a debug pause. Completes the current event's processing and records the result in history. The session remains active for inspection.",
}

type debugContinueInput struct{}

func (s *Server) handleDebugContinue(_ context.Context, _ *mcp.CallToolRequest, _ debugContinueInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.session == nil {
		return toolError("no active session"), nil, nil
	}
	if !s.session.paused {
		return toolError("session is not paused"), nil, nil
	}

	sess := s.session
	eventJSON := sess.pausedEvent

	sess.runtime.Continue()

	// Wait for next breakpoint or feed completion
	select {
	case breakInfo := <-sess.breakCh:
		// Hit another breakpoint - stay paused
		debugContext := s.collectDebugContext(sess, breakInfo)
		debugContext["paused"] = true
		return toolResult(debugContext), nil, nil

	case outcome := <-sess.feedDone:
		// Handler finished
		sess.paused = false
		sess.feedDone = nil
		sess.pausedEvent = ""
		sess.breakCh = nil

		if outcome.err != nil {
			sess.stats.Errors++
			sess.stats.Status = "error"
			sess.lastError = outcome.err
			_, _ = sess.history.Insert(eventJSON, `{"status":"error"}`)
			return toolResult(map[string]any{
				"paused":    false,
				"completed": true,
				"feedError": classifyError(outcome.err),
			}), nil, nil
		}

		resultJSON, _ := json.Marshal(outcome.result)
		_, _ = sess.history.Insert(eventJSON, string(resultJSON))
		s.recordResult(sess, outcome.result)
		sess.stats.Status = "completed"

		return toolResult(map[string]any{
			"paused":    false,
			"completed": true,
			"status":    outcome.result.Status,
		}), nil, nil
	}
}

type feedOutcome struct {
	result *gafferruntime.FeedResult
	err    error
}

func (s *Server) recordResult(sess *activeSession, result *gafferruntime.FeedResult) {
	if result.Status == "skipped" {
		sess.stats.Skipped++
	} else {
		sess.stats.Processed++
		if result.Partition != "" {
			sess.partitions[result.Partition] = true
		}
	}
}

func (s *Server) collectDebugContext(sess *activeSession, info gafferruntime.BreakInfo) map[string]any {
	result := map[string]any{
		"breakpoint": map[string]any{
			"reason": info.Reason,
			"source": info.Source,
			"line":   info.Line,
			"column": info.Column,
		},
	}

	// Call stack
	frames, err := sess.runtime.GetCallStack()
	if err == nil {
		result["callStack"] = frames
	}

	// Scopes and variables for the top frame only, excluding Global (too noisy)
	if len(frames) > 0 {
		scopes, err := sess.runtime.GetScopes(frames[0].ID)
		if err == nil {
			scopeData := []map[string]any{}
			for _, scope := range scopes {
				if scope.Name == "Global" {
					continue
				}
				vars, err := sess.runtime.GetVariables(scope.VariablesReference)
				if err != nil {
					continue
				}
				scopeData = append(scopeData, map[string]any{
					"scope":     scope.Name,
					"variables": vars,
				})
			}
			result["scopes"] = scopeData
		}
	}

	// Current state
	stateSummary := s.buildStateSummary(sess)
	for k, v := range stateSummary {
		result[k] = v
	}

	return result
}

var stepOverTool = &mcp.Tool{
	Name:        "debug_step_over",
	Description: "Step over the current statement while paused. Advances to the next statement in the same scope and returns the updated debug context.",
}

var stepIntoTool = &mcp.Tool{
	Name:        "debug_step_into",
	Description: "Step into the next function call while paused. If the current statement contains a function call, enters it. Returns the updated debug context.",
}

var stepOutTool = &mcp.Tool{
	Name:        "debug_step_out",
	Description: "Step out of the current function while paused. Continues until the current function returns and pauses in the caller. Returns the updated debug context.",
}

type debugStepInput struct{}

func (s *Server) handleStepOver(_ context.Context, _ *mcp.CallToolRequest, _ debugStepInput) (*mcp.CallToolResult, any, error) {
	return s.doStep(func(sess *activeSession) { sess.runtime.StepOver() })
}

func (s *Server) handleStepInto(_ context.Context, _ *mcp.CallToolRequest, _ debugStepInput) (*mcp.CallToolResult, any, error) {
	return s.doStep(func(sess *activeSession) { sess.runtime.StepInto() })
}

func (s *Server) handleStepOut(_ context.Context, _ *mcp.CallToolRequest, _ debugStepInput) (*mcp.CallToolResult, any, error) {
	return s.doStep(func(sess *activeSession) { sess.runtime.StepOut() })
}

func (s *Server) doStep(stepFn func(*activeSession)) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.session == nil {
		return toolError("no active session"), nil, nil
	}
	if !s.session.paused {
		return toolError("session is not paused"), nil, nil
	}

	sess := s.session

	// Issue the step command - runtime advances and re-pauses
	stepFn(sess)

	// Wait for break (step completed) or feed completion (handler finished)
	select {
	case breakInfo := <-sess.breakCh:
		debugContext := s.collectDebugContext(sess, breakInfo)
		debugContext["paused"] = true
		return toolResult(debugContext), nil, nil

	case outcome := <-sess.feedDone:
		// Step caused the handler to finish - event processing complete
		eventJSON := sess.pausedEvent

		sess.paused = false
		sess.feedDone = nil
		sess.pausedEvent = ""
		sess.breakCh = nil

		if outcome.err != nil {
			sess.stats.Errors++
			sess.stats.Status = "error"
			sess.lastError = outcome.err
			_, _ = sess.history.Insert(eventJSON, `{"status":"error"}`)
			return toolResult(map[string]any{
				"paused":    false,
				"completed": true,
				"feedError": classifyError(outcome.err),
			}), nil, nil
		}

		resultJSON, _ := json.Marshal(outcome.result)
		_, _ = sess.history.Insert(eventJSON, string(resultJSON))
		s.recordResult(sess, outcome.result)
		sess.stats.Status = "completed"

		return toolResult(map[string]any{
			"paused":    false,
			"completed": true,
			"status":    outcome.result.Status,
		}), nil, nil
	}
}
