package mcpserver

import (
	"context"
	"time"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
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
			if sess.runner.Faulted() {
				return waitResult{err: sess.runner.LastError()}
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
		debugContext := s.collectDebugContext(sess, *wr.breakInfo)
		debugContext["paused"] = true
		return toolResult(debugContext), nil, nil
	}

	if wr.caughtUp {
		return toolResult(map[string]any{
			"caughtUp":  true,
			"message":   "Subscription caught up to the head of the stream without hitting a breakpoint.",
			"processed": sess.handled(),
			"skipped":   sess.skipped(),
		}), nil, nil
	}

	if wr.completed {
		summary := stateSummaryToMap(sess.runner.CollectState())
		summary["completed"] = true
		summary["processed"] = sess.handled()
		summary["skipped"] = sess.skipped()
		summary["errors"] = sess.errors()
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
	if !s.session.runner.Paused() {
		return toolError("session is not paused - call run with break_at or breakpoints first"), nil, nil
	}
	if input.Expression == "" {
		return toolError("expression is required"), nil, nil
	}

	variable, err := s.session.runner.Evaluate(input.Expression)
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
	if !s.session.runner.Paused() {
		s.mu.Unlock()
		return toolError("session is not paused"), nil, nil
	}

	sess := s.session
	sess.runner.Continue()
	s.mu.Unlock()

	wr := s.waitForBreak(ctx, sess, defaultDebugTimeout)

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handleWaitResult(sess, wr)
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

	frames, err := sess.runner.GetCallStack()
	if err == nil {
		result["callStack"] = frames
	}

	if len(frames) > 0 {
		scopes, err := sess.runner.GetScopes(frames[0].ID)
		if err == nil {
			scopeData := []map[string]any{}
			for _, scope := range scopes {
				if scope.Name == "Global" {
					continue
				}
				vars, err := sess.runner.GetVariables(scope.VariablesReference)
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

	stateSummary := stateSummaryToMap(sess.runner.CollectState())
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
	return s.doStep(ctx, func(r *engine.Runner) { r.StepOver() })
}

func (s *Server) handleStepInto(ctx context.Context, _ *mcp.CallToolRequest, _ debugStepInput) (*mcp.CallToolResult, any, error) {
	return s.doStep(ctx, func(r *engine.Runner) { r.StepInto() })
}

func (s *Server) handleStepOut(ctx context.Context, _ *mcp.CallToolRequest, _ debugStepInput) (*mcp.CallToolResult, any, error) {
	return s.doStep(ctx, func(r *engine.Runner) { r.StepOut() })
}

func (s *Server) doStep(ctx context.Context, stepFn func(*engine.Runner)) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()

	if s.session == nil {
		s.mu.Unlock()
		return toolError("no active session"), nil, nil
	}
	if !s.session.runner.Paused() {
		s.mu.Unlock()
		return toolError("session is not paused"), nil, nil
	}

	sess := s.session
	stepFn(sess.runner)
	s.mu.Unlock()

	wr := s.waitForBreak(ctx, sess, defaultDebugTimeout)

	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handleWaitResult(sess, wr)
}
