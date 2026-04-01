package mcpserver

import (
	"context"
	"time"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultDebugTimeout = 30 * time.Second

type waitResult struct {
	breakInfo *gafferruntime.BreakInfo
	caughtUp  bool
	completed bool
	err       error
}

// waitForBreak blocks until a breakpoint is hit, the session completes,
// catches up (live), errors, or the context is cancelled. The caller must
// NOT hold s.mu - this blocks and other goroutines need the lock.
func (s *Server) waitForBreak(ctx context.Context, sess *activeSession, timeout time.Duration) waitResult {
	if timeout == 0 {
		timeout = defaultDebugTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case info := <-sess.breakCh:
			return waitResult{breakInfo: &info}

		case <-sess.done:
			// Background goroutine finished (fixture completed or errored)
			if sess.lastError != nil {
				return waitResult{err: sess.lastError}
			}
			return waitResult{completed: true}

		case <-sess.caughtUpCh:
			return waitResult{caughtUp: true}

		case err := <-sess.errorCh:
			return waitResult{err: err}

		case <-timer.C:
			return waitResult{err: context.DeadlineExceeded}

		case <-ctx.Done():
			return waitResult{err: ctx.Err()}
		}
	}
}

// handleWaitResult converts a waitResult into an MCP tool response.
// If a breakpoint was hit, returns the debug context. Otherwise returns
// status information. Must be called with s.mu held.
func (s *Server) handleWaitResult(sess *activeSession, wr waitResult) (*mcp.CallToolResult, any, error) {
	if wr.breakInfo != nil {
		sess.paused.Store(true)
		debugContext := s.collectDebugContext(sess, *wr.breakInfo)
		debugContext["paused"] = true
		return toolResult(debugContext), nil, nil
	}

	if wr.caughtUp {
		return toolResult(map[string]any{
			"caughtUp":  true,
			"message":   "Subscription caught up to the head of the stream without hitting a breakpoint.",
			"processed": sess.stats.Processed,
			"skipped":   sess.stats.Skipped,
		}), nil, nil
	}

	if wr.completed {
		summary := s.buildStateSummary(sess)
		summary["completed"] = true
		summary["processed"] = sess.stats.Processed
		summary["skipped"] = sess.stats.Skipped
		summary["errors"] = sess.stats.Errors
		return toolResult(summary), nil, nil
	}

	if wr.err == context.DeadlineExceeded {
		return toolError("timed out waiting for breakpoint"), nil, nil
	}
	if wr.err == context.Canceled {
		return toolError("cancelled"), nil, nil
	}
	if wr.err != nil {
		return toolResult(map[string]any{
			"error":     true,
			"lastError": classifyError(wr.err),
		}), nil, nil
	}

	return toolError("unexpected state"), nil, nil
}

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

func (s *Server) handleDebugContinue(ctx context.Context, _ *mcp.CallToolRequest, _ debugContinueInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()

	if s.session == nil {
		s.mu.Unlock()
		return toolError("no active session"), nil, nil
	}
	if !s.session.paused.Load() {
		s.mu.Unlock()
		return toolError("session is not paused"), nil, nil
	}

	sess := s.session
	sess.paused.Store(false)
	sess.runtime.Continue()
	s.mu.Unlock()

	wr := s.waitForBreak(ctx, sess, defaultDebugTimeout)

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handleWaitResult(sess, wr)
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

func (s *Server) handleStepOver(ctx context.Context, _ *mcp.CallToolRequest, _ debugStepInput) (*mcp.CallToolResult, any, error) {
	return s.doStep(ctx, func(sess *activeSession) { sess.runtime.StepOver() })
}

func (s *Server) handleStepInto(ctx context.Context, _ *mcp.CallToolRequest, _ debugStepInput) (*mcp.CallToolResult, any, error) {
	return s.doStep(ctx, func(sess *activeSession) { sess.runtime.StepInto() })
}

func (s *Server) handleStepOut(ctx context.Context, _ *mcp.CallToolRequest, _ debugStepInput) (*mcp.CallToolResult, any, error) {
	return s.doStep(ctx, func(sess *activeSession) { sess.runtime.StepOut() })
}

func (s *Server) doStep(ctx context.Context, stepFn func(*activeSession)) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()

	if s.session == nil {
		s.mu.Unlock()
		return toolError("no active session"), nil, nil
	}
	if !s.session.paused.Load() {
		s.mu.Unlock()
		return toolError("session is not paused"), nil, nil
	}

	sess := s.session
	sess.paused.Store(false)
	stepFn(sess)
	s.mu.Unlock()

	wr := s.waitForBreak(ctx, sess, defaultDebugTimeout)

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handleWaitResult(sess, wr)
}
