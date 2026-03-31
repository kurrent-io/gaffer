package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/history"
	"github.com/kurrent-io/gaffer/cli/internal/projection"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var debugTool = &mcp.Tool{
	Name:        "debug",
	Description: "Run a projection with debug enabled, pausing at a specific event position. Returns the full debug context at that point: state, call stack, scopes, and variables. Also populates session history up to that position for inspection with other tools.",
}

type debugInput struct {
	Name    string `json:"name" jsonschema:"Projection name from gaffer.toml"`
	Events  string `json:"events" jsonschema:"Path to a JSON fixture file (relative to project root or absolute)"`
	BreakAt int64  `json:"break_at" jsonschema:"Event position (1-based) to pause at"`
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
	sess, err := s.createDebugSession(input.Name)
	if err != nil {
		if projErr, ok := err.(gafferruntime.ProjectionError); ok {
			return toolResult(map[string]any{
				"error": projErr.Error(),
				"code":  projErr.ErrorCode(),
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

	// Set up break signaling
	var breakOnce sync.Once
	breakCh := make(chan gafferruntime.BreakInfo, 1)
	sess.runtime.OnBreak(func(info gafferruntime.BreakInfo) {
		breakOnce.Do(func() {
			breakCh <- info
		})
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

	// Pause before the target event
	sess.runtime.Pause()

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

		sess.runtime.Continue()
		outcome := <-feedDone

		if outcome.err != nil {
			debugContext["feedError"] = classifyError(outcome.err)
			_, _ = sess.history.Insert(targetEvent, `{"status":"error"}`)
		} else {
			resultJSON, _ := json.Marshal(outcome.result)
			_, _ = sess.history.Insert(targetEvent, string(resultJSON))
			s.recordResult(sess, outcome.result)
		}

		sess.stats.Status = "completed"
		debugContext["position"] = input.BreakAt
		debugContext["totalEvents"] = len(events)
		return toolResult(debugContext), nil, nil

	case outcome := <-feedDone:
		// Feed completed without breaking - event was skipped or errored
		result := map[string]any{
			"position":    input.BreakAt,
			"totalEvents": len(events),
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

func (s *Server) createDebugSession(name string) (*activeSession, error) {
	s.closeSession()

	proj := s.cfg.FindProjection(name)
	if proj == nil {
		return nil, fmt.Errorf("projection %q not found in gaffer.toml", name)
	}

	source, err := readProjectionSource(s.root, proj.Entry)
	if err != nil {
		return nil, err
	}

	opts := projection.BuildSessionOptions(s.cfg, proj, true)
	runtime, err := gafferruntime.NewSession(string(source), opts)
	if err != nil {
		return nil, err
	}

	store, err := history.New()
	if err != nil {
		runtime.Destroy()
		return nil, err
	}

	info := projection.GetInfo(runtime)

	s.session = &activeSession{
		runtime:    runtime,
		history:    store,
		info:       info,
		name:       name,
		partitions: make(map[string]bool),
		stats:      sessionStats{Status: "debugging"},
	}

	return s.session, nil
}
