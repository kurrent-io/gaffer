package mcpserver

import (
	"context"
	"encoding/json"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var evaluateTool = &mcp.Tool{
	Name:        "evaluate",
	Description: "Evaluate a JavaScript expression in the current debug context. Only works while paused at a breakpoint (after run with break_at or breakpoints). Returns the expression result with type information.",
}

type evaluateInput struct {
	Expression string `json:"expression" jsonschema:"JavaScript expression to evaluate in the current scope"`
}

func (s *Server) handleEvaluate(_ context.Context, _ *mcp.CallToolRequest, input evaluateInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.session == nil {
		return toolError("no active session - call run first"), nil, nil
	}
	if !s.session.paused.Load() {
		return toolError("session is not paused - call run with break_at or breakpoints first"), nil, nil
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
	if !s.session.paused.Load() {
		return toolError("session is not paused"), nil, nil
	}

	sess := s.session

	// Live debug mode - resume and return immediately.
	// The subscription goroutine resumes feeding. Agent polls status for next break.
	if sess.feedDone == nil {
		sess.paused.Store(false)
		sess.stats.Status = "running"
		sess.runtime.Continue()
		return toolResult(map[string]any{
			"resumed": true,
			"mode":    "live",
			"hint":    "Subscription resumed. Poll status for the next breakpoint_hit.",
		}), nil, nil
	}

	// Fixture debug mode - wait for next breakpoint or feed completion
	eventJSON := sess.pausedEvent
	sess.runtime.Continue()

	select {
	case breakInfo := <-sess.breakCh:
		debugContext := s.collectDebugContext(sess, breakInfo)
		debugContext["paused"] = true
		return toolResult(debugContext), nil, nil

	case outcome := <-sess.feedDone:
		sess.paused.Store(false)
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
	if !s.session.paused.Load() {
		return toolError("session is not paused"), nil, nil
	}

	sess := s.session

	// Issue the step command - the C runtime advances and re-pauses.
	stepFn(sess)

	// For live/fixture-debug-run mode (no feedDone), return immediately.
	// The background goroutine handles its own state. Agent polls status.
	if sess.feedDone == nil {
		sess.paused.Store(false)
		sess.stats.Status = "running"
		return toolResult(map[string]any{
			"stepped": true,
			"hint":    "Step issued. Poll status for the next breakpoint_hit or completion.",
		}), nil, nil
	}

	// For debug-tool mode (has feedDone), wait for break or completion.
	select {
	case breakInfo := <-sess.breakCh:
		sess.paused.Store(true)
		sess.stats.Status = "breakpoint_hit"
		debugContext := s.collectDebugContext(sess, breakInfo)
		debugContext["paused"] = true
		return toolResult(debugContext), nil, nil

	case outcome := <-sess.feedDone:
		eventJSON := sess.pausedEvent

		sess.paused.Store(false)
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
