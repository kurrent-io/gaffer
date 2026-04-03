package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/history"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerTools() {
	mcp.AddTool(s.mcp, validateTool, s.handleValidate)
	mcp.AddTool(s.mcp, runTool, s.handleRun)
	mcp.AddTool(s.mcp, stopTool, s.handleStop)
	mcp.AddTool(s.mcp, getStepTool, s.handleGetStep)
	mcp.AddTool(s.mcp, getHistoryTool, s.handleGetHistory)
	mcp.AddTool(s.mcp, getTimelineTool, s.handleGetTimeline)
	mcp.AddTool(s.mcp, getStateTool, s.handleGetState)
	mcp.AddTool(s.mcp, listProjectionsTool, s.handleListProjections)
	mcp.AddTool(s.mcp, scaffoldTool, s.handleScaffold)
	mcp.AddTool(s.mcp, evaluateTool, s.handleEvaluate)
	mcp.AddTool(s.mcp, debugContinueTool, s.handleDebugContinue)
	mcp.AddTool(s.mcp, stepOverTool, s.handleStepOver)
	mcp.AddTool(s.mcp, stepIntoTool, s.handleStepInto)
	mcp.AddTool(s.mcp, stepOutTool, s.handleStepOut)
	mcp.AddTool(s.mcp, listEventsTool, s.handleListEvents)
}

// --- Tool definitions ---

var validateTool = &mcp.Tool{
	Name:        "validate",
	Description: "Compile and check a projection without running it. Returns whether the source is valid and projection metadata (source type, events, partitioning). Does not create or affect any session.",
}

var runTool = &mcp.Tool{
	Name:        "run",
	Description: "Run a projection against events. Creates a new session (replacing any existing one). Always blocks until completion. Fixture mode: returns summary when all events are consumed. Live mode: blocks until caught_up, error, or timeout. Set breakpoints (source lines) or break_at (event position) to pause at specific points - returns debug context with call stack, variables, and state.",
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

var getStateTool = &mcp.Tool{
	Name:        "get_state",
	Description: "Get the current projection state from the active session. Returns state per partition (or global state if unpartitioned), shared state if biState, and result if transforms are defined.",
}

type getStateInput struct {
	Partition string `json:"partition,omitempty" jsonschema:"Get state for a specific partition. Omit for all partitions or global state."`
}

var listProjectionsTool = &mcp.Tool{
	Name:        "list_projections",
	Description: "List all projections defined in the project's gaffer.toml.",
}

// --- Input types ---

type validateInput struct {
	Name string `json:"name" jsonschema:"Projection name from gaffer.toml"`
}

type breakpointInput struct {
	Line      int    `json:"line" jsonschema:"Source line number (1-based)"`
	Condition string `json:"condition,omitempty" jsonschema:"JS expression that must be truthy for the breakpoint to fire"`
}

type runInput struct {
	Name        string            `json:"name" jsonschema:"Projection name from gaffer.toml"`
	Events      string            `json:"events,omitempty" jsonschema:"Path to a JSON fixture file (relative to project root or absolute). Omit for live KurrentDB subscription."`
	Breakpoints []breakpointInput `json:"breakpoints,omitempty" jsonschema:"Source line breakpoints to set before feeding events. Enables debug mode."`
	BreakAt     int64             `json:"break_at,omitempty" jsonschema:"Pause at a specific event position (1-based). Enables debug mode."`
}

type stopInput struct{}

type getStepInput struct {
	Position int64 `json:"position" jsonschema:"Event position (1-based) from the session history"`
}

type getHistoryInput struct {
	From      int64  `json:"from" jsonschema:"Start position (inclusive). Defaults to 1 if 0."`
	To        int64  `json:"to" jsonschema:"End position (inclusive). Defaults to last position if 0."`
	Partition string `json:"partition,omitempty" jsonschema:"Filter to a specific partition key"`
}

type getTimelineInput struct {
	From      int64  `json:"from" jsonschema:"Start position (inclusive). Defaults to 1 if 0."`
	To        int64  `json:"to" jsonschema:"End position (inclusive). Defaults to last position if 0."`
	Partition string `json:"partition,omitempty" jsonschema:"Filter to a specific partition key"`
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

	lp := engine.NewLoadedProjection(s.root, s.cfg, proj, string(source))
	session, info, err := engine.NewSession(lp, false)
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

	return toolResult(map[string]any{
		"valid":           true,
		"name":            input.Name,
		"entry":           proj.Entry,
		"engine":          proj.EffectiveEngine(),
		"source":          describeSource(info),
		"events":          info.Events,
		"partitioning":    describePartitioning(info),
		"biState":         info.IsBiState,
		"producesResults": info.ProducesResults,
	}), nil, nil
}

func (s *Server) handleRun(ctx context.Context, _ *mcp.CallToolRequest, input runInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()

	debug := len(input.Breakpoints) > 0 || input.BreakAt > 0

	sess, err := s.createSession(input.Name, debug)
	if err != nil {
		s.mu.Unlock()
		if _, ok := err.(gafferruntime.ProjectionError); ok {
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
		if err := s.startLiveMode(sess, input.BreakAt); err != nil {
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
		if err := s.runFixtureDebugMode(sess, input.Events, input.BreakAt); err != nil {
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
	return s.runFixtureMode(sess, input.Events)
}

func (s *Server) setupBreakpoints(sess *activeSession, breakpoints []breakpointInput) error {
	for _, bp := range breakpoints {
		var opts *gafferruntime.BreakpointOptions
		if bp.Condition != "" {
			opts = &gafferruntime.BreakpointOptions{Condition: bp.Condition}
		}
		snapped, err := sess.runtime.SetBreakpoint(bp.Line, 0, opts)
		if err != nil {
			return fmt.Errorf("setting breakpoint at line %d: %w", bp.Line, err)
		}
		if snapped == nil {
			return fmt.Errorf("no breakable position at or after line %d", bp.Line)
		}
	}

	breakCh := make(chan gafferruntime.BreakInfo, 1)
	sess.breakCh = breakCh
	sess.caughtUpCh = make(chan struct{}, 1)
	sess.errorCh = make(chan error, 1)

	sess.runtime.OnBreak(func(info gafferruntime.BreakInfo) {
		// When break_at is used, Pause() fires at handler entry where
		// params aren't in scope yet. Auto-step into the handler body
		// so the agent gets state/event available for evaluate.
		if info.Reason == "pause" && sess.breakAtPosition > 0 {
			go sess.runtime.StepInto()
			return
		}

		sess.paused.Store(true)
		select {
		case breakCh <- info:
		default:
		}
	})

	return nil
}

func (s *Server) startLiveMode(sess *activeSession, breakAt int64) error {
	if breakAt > 0 {
		sess.breakAtPosition = breakAt
	}
	return s.startLiveSubscription(sess)
}

func (s *Server) runFixtureDebugMode(sess *activeSession, eventsPath string, breakAt int64) error {
	if !filepath.IsAbs(eventsPath) {
		eventsPath = filepath.Join(s.root, eventsPath)
	}

	events, err := engine.LoadEvents(eventsPath)
	if err != nil {
		return err
	}

	if breakAt > int64(len(events)) {
		return fmt.Errorf("break_at %d exceeds total events (%d)", breakAt, len(events))
	}

	done := make(chan struct{})
	sess.done = done

	go func() {
		defer close(done)
		for i, evt := range events {
			position := int64(i + 1)
			if breakAt > 0 && position == breakAt {
				sess.pausedEvent = evt
				sess.runtime.Pause()
			}

			result, feedErr := sess.runtime.Feed(evt)

			s.mu.Lock()
			if feedErr != nil {
				sess.stats.Errors++
				sess.stats.Status = "error"
				sess.lastError = feedErr
				_, _ = sess.history.Insert(evt, `{"status":"error"}`)
				s.mu.Unlock()
				return
			}

			resultJSON, _ := json.Marshal(result)
			_, _ = sess.history.Insert(evt, string(resultJSON))
			s.recordResult(sess, result)
			s.mu.Unlock()
		}

		s.mu.Lock()
		if sess.stats.Status != "error" {
			sess.stats.Status = "completed"
		}
		s.mu.Unlock()
	}()

	return nil
}

func (s *Server) runFixtureMode(sess *activeSession, eventsPath string) (*mcp.CallToolResult, any, error) {
	if !filepath.IsAbs(eventsPath) {
		eventsPath = filepath.Join(s.root, eventsPath)
	}

	events, err := engine.LoadEvents(eventsPath)
	if err != nil {
		return toolError("%v", err), nil, nil
	}

	ew := &errorCapture{}
	sess.runner = engine.NewRunner(engine.RunnerConfig{
		Feed:    engine.FeedFn(sess.runtime.Feed),
		Writer:  ew,
		History: sess.history,
	})
	source := engine.NewFixtureSource(events)
	_ = source.Run(context.Background(), sess.runner.ProcessOne)

	if !sess.runner.Faulted {
		sess.stats.Status = "completed"
	} else {
		sess.stats.Status = "error"
	}

	summary := stateSummaryToMap(engine.CollectState(sess.runtime, sess.info, sess.activePartitions()))
	summary["completed"] = !sess.runner.Faulted
	summary["processed"] = sess.handled()
	summary["skipped"] = sess.skipped()
	summary["errors"] = sess.errors()
	summary["totalEvents"] = len(events)

	if sess.runner.Faulted && ew.lastError != nil {
		summary["lastError"] = ew.lastError
	}

	return toolResult(summary), nil, nil
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

	timeline, err := s.session.history.TimelineFiltered(from, to, input.Partition)
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

	entries, err := s.session.history.TimelineFiltered(from, to, input.Partition)
	if err != nil {
		return toolError("querying timeline: %v", err), nil, nil
	}

	return toolResult(map[string]any{
		"entries": entries,
	}), nil, nil
}

func (s *Server) handleGetState(_ context.Context, _ *mcp.CallToolRequest, input getStateInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.session == nil {
		return toolError("no active session - call run first"), nil, nil
	}

	sess := s.session

	if input.Partition != "" {
		result := map[string]any{"partition": input.Partition}
		if state := sess.runtime.GetState(&input.Partition); state != nil {
			result["state"] = json.RawMessage(*state)
		}
		if sess.info.DefinesStateTransform {
			if r, err := sess.runtime.GetResult(&input.Partition); err == nil && r != nil {
				result["result"] = json.RawMessage(*r)
			}
		}
		return toolResult(result), nil, nil
	}

	return toolResult(stateSummaryToMap(engine.CollectState(sess.runtime, sess.info, sess.activePartitions()))), nil, nil
}

func (s *Server) handleListProjections(_ context.Context, _ *mcp.CallToolRequest, _ listProjectionsInput) (*mcp.CallToolResult, any, error) {
	projections := []map[string]any{}
	for _, proj := range s.cfg.Projection {
		entry := map[string]any{
			"name":   proj.Name,
			"entry":  proj.Entry,
			"engine": proj.EffectiveEngine(),
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

func stateSummaryToMap(s engine.StateSummary) map[string]any {
	result := map[string]any{}

	if s.Partitioned {
		partitions := map[string]any{}
		for name, ps := range s.Partitions {
			pd := map[string]any{}
			if len(ps.State) > 0 {
				pd["state"] = json.RawMessage(ps.State)
			}
			if s.HasTransforms && len(ps.Result) > 0 {
				pd["result"] = json.RawMessage(ps.Result)
			}
			partitions[name] = pd
		}
		result["partitions"] = partitions
	} else {
		if len(s.State) > 0 {
			result["state"] = json.RawMessage(s.State)
		}
		if s.HasTransforms && len(s.Result) > 0 {
			result["result"] = json.RawMessage(s.Result)
		}
	}

	if s.HasBiState && len(s.SharedState) > 0 {
		result["sharedState"] = json.RawMessage(s.SharedState)
	}

	return result
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

func describeSource(info gafferruntime.QuerySources) map[string]any {
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

type errorCapture struct {
	lastError map[string]any
}

func (e *errorCapture) OnEvent(string)                             {}
func (e *errorCapture) OnResult(string, *gafferruntime.FeedResult) {}
func (e *errorCapture) OnError(_, code, description string) {
	result := map[string]any{
		"code":        code,
		"description": description,
	}
	if hint := errorHint(code); hint != "" {
		result["hint"] = hint
	}
	e.lastError = result
}

func describePartitioning(info gafferruntime.QuerySources) string {
	if info.ByStreams {
		return "byStream"
	}
	if info.ByCustomPartitions {
		return "byCustomKey"
	}
	return "none"
}
