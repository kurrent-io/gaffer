package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/history"
	"github.com/kurrent-io/gaffer/cli/internal/projection"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerTools() {
	mcp.AddTool(s.mcp, validateTool, s.handleValidate)
	mcp.AddTool(s.mcp, runTool, s.handleRun)
	mcp.AddTool(s.mcp, statusTool, s.handleStatus)
	mcp.AddTool(s.mcp, stopTool, s.handleStop)
	mcp.AddTool(s.mcp, getStepTool, s.handleGetStep)
	mcp.AddTool(s.mcp, getHistoryTool, s.handleGetHistory)
	mcp.AddTool(s.mcp, getTimelineTool, s.handleGetTimeline)
	mcp.AddTool(s.mcp, listProjectionsTool, s.handleListProjections)
	mcp.AddTool(s.mcp, scaffoldTool, s.handleScaffold)
	mcp.AddTool(s.mcp, debugTool, s.handleDebug)
	mcp.AddTool(s.mcp, evaluateTool, s.handleEvaluate)
	mcp.AddTool(s.mcp, debugContinueTool, s.handleDebugContinue)
	mcp.AddTool(s.mcp, listEventsTool, s.handleListEvents)
}

// --- Tool definitions ---

var validateTool = &mcp.Tool{
	Name:        "validate",
	Description: "Compile and check a projection without running it. Returns whether the source is valid and projection metadata (source type, events, partitioning). Does not create or affect any session.",
}

var runTool = &mcp.Tool{
	Name:        "run",
	Description: "Run a projection against events. Creates a new session (replacing any existing one) and populates history for inspection. With events file: runs synchronously and returns summary. Without events file: starts a live KurrentDB subscription in the background - poll status to track progress.",
}

var statusTool = &mcp.Tool{
	Name:        "status",
	Description: "Get the status of the active session, including event counts and current position.",
}

var stopTool = &mcp.Tool{
	Name:        "stop",
	Description: "Stop and tear down the active session.",
}

var getStepTool = &mcp.Tool{
	Name:        "get_step",
	Description: "Get full detail for a specific step in the active session's history. Returns the event, status, state, emitted events, and logs.",
}

var getHistoryTool = &mcp.Tool{
	Name:        "get_history",
	Description: "Get state snapshots and a compact step summary between two positions. Returns the projection state before the range, the state after the range, and timeline entries for each step in between. Use get_step for full event/result detail at a specific position.",
}

var getTimelineTool = &mcp.Tool{
	Name:        "get_timeline",
	Description: "Get a compact overview of a range of steps. Returns position, event type, stream ID, status, and flags for each step. Use this to scan for interesting positions, then drill in with get_step.",
}

var listProjectionsTool = &mcp.Tool{
	Name:        "list_projections",
	Description: "List all projections defined in the project's gaffer.toml.",
}

// --- Input types ---

type validateInput struct {
	Name string `json:"name" jsonschema:"Projection name from gaffer.toml"`
}

type runInput struct {
	Name   string `json:"name" jsonschema:"Projection name from gaffer.toml"`
	Events string `json:"events,omitempty" jsonschema:"Path to a JSON fixture file (relative to project root or absolute). Omit for live KurrentDB subscription."`
}

type statusInput struct{}

type stopInput struct{}

type getStepInput struct {
	Position int64 `json:"position" jsonschema:"Event position (1-based) from the session history"`
}

type getHistoryInput struct {
	From int64 `json:"from" jsonschema:"Start position (inclusive). Defaults to 1 if 0."`
	To   int64 `json:"to" jsonschema:"End position (inclusive). Defaults to last position if 0."`
}

type getTimelineInput struct {
	From int64 `json:"from" jsonschema:"Start position (inclusive). Defaults to 1 if 0."`
	To   int64 `json:"to" jsonschema:"End position (inclusive). Defaults to last position if 0."`
}

type listProjectionsInput struct{}

// --- Handlers ---

func (s *Server) handleValidate(_ context.Context, _ *mcp.CallToolRequest, input validateInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	proj := s.cfg.FindProjection(input.Name)
	if proj == nil {
		return toolError("projection %q not found in gaffer.toml", input.Name), nil, nil
	}

	source, err := readProjectionSource(s.root, proj.Entry)
	if err != nil {
		return toolError("%v", err), nil, nil
	}

	opts := projection.BuildSessionOptions(s.cfg, proj, false)
	session, err := gafferruntime.NewSession(string(source), opts)
	if err != nil {
		if _, ok := err.(gafferruntime.ProjectionError); ok {
			return toolResult(map[string]any{
				"valid":     false,
				"lastError": classifyError(err),
			}), nil, nil
		}
		return toolError("creating session: %v", err), nil, nil
	}
	defer session.Destroy()

	info := projection.GetInfo(session)

	return toolResult(map[string]any{
		"valid":           true,
		"name":            input.Name,
		"entry":           proj.Entry,
		"engine":          engineOrDefault(proj.Engine),
		"source":          describeSource(info),
		"events":          info.Events,
		"partitioning":    describePartitioning(info),
		"biState":         info.IsBiState,
		"producesResults": info.ProducesResults,
	}), nil, nil
}

func (s *Server) handleRun(_ context.Context, _ *mcp.CallToolRequest, input runInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, err := s.createSession(input.Name, false)
	if err != nil {
		if _, ok := err.(gafferruntime.ProjectionError); ok {
			return toolResult(map[string]any{
				"lastError": classifyError(err),
			}), nil, nil
		}
		return toolError("%v", err), nil, nil
	}

	if input.Events == "" {
		return s.startLiveMode(sess)
	}

	return s.runFixtureMode(sess, input.Events)
}

func (s *Server) startLiveMode(sess *activeSession) (*mcp.CallToolResult, any, error) {
	if err := s.startLiveSubscription(sess); err != nil {
		return toolError("%v", err), nil, nil
	}

	return toolResult(map[string]any{
		"projection": sess.name,
		"status":     "running",
		"mode":       "live",
		"message":    "Live subscription started. Poll status to track progress. History is queryable as events are processed.",
	}), nil, nil
}

func (s *Server) runFixtureMode(sess *activeSession, eventsPath string) (*mcp.CallToolResult, any, error) {
	if !filepath.IsAbs(eventsPath) {
		eventsPath = filepath.Join(s.root, eventsPath)
	}

	events, err := projection.LoadEvents(eventsPath)
	if err != nil {
		return toolError("%v", err), nil, nil
	}

	var faultErr error
	for _, evt := range events {
		result, feedErr := sess.runtime.Feed(evt)
		if feedErr != nil {
			sess.stats.Errors++
			sess.stats.Status = "error"
			faultErr = feedErr
			_, _ = sess.history.Insert(evt, `{"status":"error"}`)

			break
		}

		resultJSON, _ := json.Marshal(result)

		if _, insertErr := sess.history.Insert(evt, string(resultJSON)); insertErr != nil {
			return toolError("recording history: %v", insertErr), nil, nil
		}

		s.recordResult(sess, result)
	}

	if faultErr == nil {
		sess.stats.Status = "completed"
	}

	summary := s.buildStateSummary(sess)
	summary["processed"] = sess.stats.Processed
	summary["skipped"] = sess.stats.Skipped
	summary["errors"] = sess.stats.Errors
	summary["status"] = sess.stats.Status
	summary["totalEvents"] = len(events)

	if faultErr != nil {
		summary["lastError"] = classifyError(faultErr)
	}

	return toolResult(summary), nil, nil
}

func (s *Server) handleStatus(_ context.Context, _ *mcp.CallToolRequest, _ statusInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.session == nil {
		return toolError("no active session - call run first"), nil, nil
	}

	count, _ := s.session.history.Count()
	minPos, maxPos, _ := s.session.history.Range()

	result := map[string]any{
		"projection":  s.session.name,
		"status":      s.session.stats.Status,
		"processed":   s.session.stats.Processed,
		"skipped":     s.session.stats.Skipped,
		"errors":      s.session.stats.Errors,
		"position":    count,
		"minPosition": minPos,
		"maxPosition": maxPos,
	}

	if s.session.lastError != nil {
		result["lastError"] = classifyError(s.session.lastError)
	}

	return toolResult(result), nil, nil
}

func (s *Server) handleStop(_ context.Context, _ *mcp.CallToolRequest, _ stopInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.session == nil {
		return toolError("no active session"), nil, nil
	}

	s.closeSession()
	return toolResult(map[string]any{"stopped": true}), nil, nil
}

func (s *Server) handleGetStep(_ context.Context, _ *mcp.CallToolRequest, input getStepInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.session == nil {
		return toolError("no active session - call run first"), nil, nil
	}

	step, err := s.session.history.Get(input.Position)
	if err != nil {
		return toolError("querying history: %v", err), nil, nil
	}
	if step == nil {
		return toolError("no step at position %d", input.Position), nil, nil
	}

	return toolResult(formatStep(step)), nil, nil
}

func (s *Server) handleGetHistory(_ context.Context, _ *mcp.CallToolRequest, input getHistoryInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.session == nil {
		return toolError("no active session - call run first"), nil, nil
	}

	from, to := s.resolveRange(input.From, input.To)

	var beforeState json.RawMessage
	if from > 1 {
		beforeStep, err := s.session.history.Get(from - 1)
		if err == nil && beforeStep != nil {
			beforeState = extractState(beforeStep.ResultJSON)
		}
	}

	afterStep, err := s.session.history.Get(to)
	if err != nil {
		return toolError("querying history: %v", err), nil, nil
	}

	var afterState json.RawMessage
	if afterStep != nil {
		afterState = extractState(afterStep.ResultJSON)
	}

	timeline, err := s.session.history.Timeline(from, to)
	if err != nil {
		return toolError("querying timeline: %v", err), nil, nil
	}

	return toolResult(map[string]any{
		"before": beforeState,
		"steps":  timeline,
		"after":  afterState,
	}), nil, nil
}

func (s *Server) handleGetTimeline(_ context.Context, _ *mcp.CallToolRequest, input getTimelineInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.session == nil {
		return toolError("no active session - call run first"), nil, nil
	}

	from, to := s.resolveRange(input.From, input.To)

	entries, err := s.session.history.Timeline(from, to)
	if err != nil {
		return toolError("querying timeline: %v", err), nil, nil
	}

	return toolResult(map[string]any{
		"entries": entries,
	}), nil, nil
}

func (s *Server) handleListProjections(_ context.Context, _ *mcp.CallToolRequest, _ listProjectionsInput) (*mcp.CallToolResult, any, error) {
	projections := []map[string]any{}
	for _, proj := range s.cfg.Projection {
		entry := map[string]any{
			"name":   proj.Name,
			"entry":  proj.Entry,
			"engine": engineOrDefault(proj.Engine),
		}
		if proj.Enabled != nil && !*proj.Enabled {
			entry["enabled"] = false
		}
		projections = append(projections, entry)
	}

	return toolResult(map[string]any{
		"projections": projections,
		"root":        s.root,
	}), nil, nil
}

// --- Helpers ---

func (s *Server) resolveRange(from, to int64) (int64, int64) {
	minPos, maxPos, _ := s.session.history.Range()
	if from <= 0 {
		from = minPos
	}
	if to <= 0 {
		to = maxPos
	}
	if from < minPos {
		from = minPos
	}
	if to < from {
		to = from
	}
	return from, to
}

func (s *Server) buildStateSummary(sess *activeSession) map[string]any {
	summary := map[string]any{}

	isPartitioned := sess.info.ByStreams || sess.info.ByCustomPartitions

	if isPartitioned {
		partitions := map[string]any{}
		for partition := range sess.partitions {
			pd := map[string]any{}
			if state := sess.runtime.GetState(&partition); state != nil {
				pd["state"] = json.RawMessage(*state)
			}
			if sess.info.DefinesStateTransform {
				if result, err := sess.runtime.GetResult(&partition); err == nil && result != nil {
					pd["result"] = json.RawMessage(*result)
				}
			}
			partitions[partition] = pd
		}
		summary["partitions"] = partitions
	} else {
		if state := sess.runtime.GetState(nil); state != nil {
			summary["state"] = json.RawMessage(*state)
		}
		if sess.info.DefinesStateTransform {
			if result, err := sess.runtime.GetResult(nil); err == nil && result != nil {
				summary["result"] = json.RawMessage(*result)
			}
		}
	}

	if sess.info.IsBiState {
		if shared := sess.runtime.GetSharedState(); shared != nil {
			summary["sharedState"] = json.RawMessage(*shared)
		}
	}

	return summary
}

func readProjectionSource(root, entry string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(root, entry))
	if err != nil {
		return nil, fmt.Errorf("reading projection source: %w", err)
	}
	return data, nil
}

func formatStep(step *history.Step) map[string]any {
	var event any
	_ = json.Unmarshal([]byte(step.EventJSON), &event)

	var result any
	_ = json.Unmarshal([]byte(step.ResultJSON), &result)

	return map[string]any{
		"position":  step.Position,
		"eventType": step.EventType,
		"streamId":  step.StreamID,
		"status":    step.Status,
		"partition": step.Partition,
		"event":     event,
		"result":    result,
	}
}

func extractState(resultJSON string) json.RawMessage {
	var obj struct {
		State json.RawMessage `json:"state"`
	}
	_ = json.Unmarshal([]byte(resultJSON), &obj)
	return obj.State
}

func toolResult(data any) *mcp.CallToolResult {
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("error marshaling result: %v", err)}},
			IsError: true,
		}
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(jsonBytes)}},
	}
}

func toolError(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
		IsError: true,
	}
}

func describeSource(info projection.Info) map[string]any {
	if info.AllStreams {
		return map[string]any{"type": "all"}
	}
	if len(info.Categories) > 0 {
		return map[string]any{"type": "categories", "categories": info.Categories}
	}
	if len(info.Streams) > 0 {
		return map[string]any{"type": "streams", "streams": info.Streams}
	}
	return map[string]any{"type": "unknown"}
}

func describePartitioning(info projection.Info) string {
	if info.ByStreams {
		return "byStream"
	}
	if info.ByCustomPartitions {
		return "byCustomKey"
	}
	return "none"
}

func engineOrDefault(engine string) string {
	if engine == "" {
		return "v2"
	}
	return engine
}
