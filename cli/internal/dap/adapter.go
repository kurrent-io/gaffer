package dap

import (
	"encoding/json"
	"path"
	"strings"
	"sync"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"

	"github.com/kurrent-io/gaffer/cli/internal/engine"

	godap "github.com/google/go-dap"
)

// DebugAdapter bridges a DAP server to an engine Runner.
type DebugAdapter struct {
	runner     *engine.Runner
	session    *gafferruntime.Session
	server     *Server
	sourcePath string
	remoteRoot string
	localRoot  string

	mu                         sync.Mutex
	inspect                    bool
	stepBuffer                 []*CustomEvent
	breakpointCount            int
	startPausedIfNoBreakpoints bool
	entryPausePending          bool
	lastStats                  engine.EventStats
	lastStateJSON              string

	readyOnce      sync.Once
	readyCh        chan struct{}
	finalStateSent bool

	// Restart coordination. handleRestart blocks the DAP read goroutine
	// while dev.go's main loop tears down the current engine session
	// and stands up a fresh one. Buffered so a click-spam doesn't drop
	// the signal; ack is unbuffered so the response is gated on real
	// readiness, not a fire-and-forget.
	restartReqCh chan struct{}
	restartAckCh chan struct{}
}

// NewDebugAdapter creates an adapter that bridges a DAP server to an engine Runner.
// sourcePath is the filesystem path to the projection JS file (for Source objects).
// remoteRoot is the project root (where gaffer.toml lives) on the server side.
// session is needed for OnLog/OnEmit callbacks (output events to the editor).
func NewDebugAdapter(session *gafferruntime.Session, sourcePath, remoteRoot string) *DebugAdapter {
	return &DebugAdapter{
		session:      session,
		sourcePath:   sourcePath,
		remoteRoot:   remoteRoot,
		readyCh:      make(chan struct{}),
		restartReqCh: make(chan struct{}, 1),
		restartAckCh: make(chan struct{}),
	}
}

// SetRunner connects the adapter to the engine runner.
func (a *DebugAdapter) SetRunner(r *engine.Runner) {
	a.runner = r
}

// SetStartPausedIfNoBreakpoints enables an entry pause when no breakpoints
// are registered by the time configurationDone arrives. The pause fires at
// the first handler invocation.
func (a *DebugAdapter) SetStartPausedIfNoBreakpoints(v bool) {
	a.mu.Lock()
	a.startPausedIfNoBreakpoints = v
	a.mu.Unlock()
}

// HandleBreak is called by the runner's OnBreak callback.
func (a *DebugAdapter) HandleBreak(info gafferruntime.BreakInfo) {
	if a.server == nil {
		return
	}

	a.mu.Lock()
	a.inspect = true
	entryPause := a.entryPausePending
	a.entryPausePending = false
	a.mu.Unlock()

	reason := info.Reason
	switch {
	case entryPause:
		reason = "entry"
	case reason == "debugger_statement":
		reason = "pause"
	}
	a.server.SendEvent(&godap.StoppedEvent{
		Event: NewEvent("stopped"),
		Body: godap.StoppedEventBody{
			Reason:            reason,
			ThreadId:          1,
			AllThreadsStopped: true,
		},
	})
	a.server.Send(NewCustomEvent("gaffer/mode", map[string]any{
		"mode": "inspect",
	}))
	a.flushStepBuffer()
}

// SetServer connects the adapter to a DAP server for sending events.
// Wires up session callbacks (OnLog, OnEmit) for output events on the
// current session.
func (a *DebugAdapter) SetServer(server *Server) {
	a.server = server
	a.wireSessionCallbacks()
}

// SetSession replaces the bound runtime session and re-wires OnLog /
// OnEmit callbacks against it. Called per-iteration in dev.go's
// session loop so a restart's freshly-spun session keeps emitting
// output / step events to the still-attached editor.
func (a *DebugAdapter) SetSession(session *gafferruntime.Session) {
	a.session = session
	a.wireSessionCallbacks()
}

func (a *DebugAdapter) wireSessionCallbacks() {
	if a.session == nil || a.server == nil {
		return
	}
	a.session.OnLog(func(message string) {
		a.server.SendEvent(&godap.OutputEvent{
			Event: NewEvent("output"),
			Body: godap.OutputEventBody{
				Category: "console",
				Output:   message + "\n",
			},
		})
		a.mu.Lock()
		inspect := a.inspect
		a.mu.Unlock()
		evt := NewCustomEvent("gaffer/stepLog", map[string]any{
			"message": message,
		})
		a.bufferOrSend(evt, inspect)
	})

	a.session.OnEmit(func(streamID, eventType, data, metadata string, isJSON, isLink bool) {
		body := map[string]any{
			"streamId":  streamID,
			"eventType": eventType,
			"isLink":    isLink,
			"isJson":    isJSON,
		}
		if data != "" {
			if isJSON {
				body["data"] = json.RawMessage(data)
			} else {
				body["data"] = data
			}
		}
		if metadata != "" {
			body["metadata"] = metadata
		}
		a.mu.Lock()
		inspect := a.inspect
		a.mu.Unlock()
		a.bufferOrSend(NewCustomEvent("gaffer/stepEmit", body), inspect)
	})
}

// EventWriter returns an engine.EventWriter that sends DAP custom events
// for each event processed by the runner.
func (a *DebugAdapter) EventWriter() engine.EventWriter {
	return &dapEventWriter{adapter: a}
}

type dapEventWriter struct {
	adapter *DebugAdapter
}

func (w *dapEventWriter) OnEvent(eventJSON string) {
	w.adapter.mu.Lock()
	w.adapter.stepBuffer = nil
	inspect := w.adapter.inspect
	w.adapter.mu.Unlock()

	evt := NewCustomEvent("gaffer/stepStart", map[string]any{
		"event": json.RawMessage(eventJSON),
	})
	w.adapter.bufferOrSend(evt, inspect)
}

func (w *dapEventWriter) OnResult(eventID string, result *gafferruntime.FeedResult) {
	resultJSON, _ := json.Marshal(result)

	w.adapter.mu.Lock()
	inspect := w.adapter.inspect
	w.adapter.mu.Unlock()

	resultEvt := NewCustomEvent("gaffer/stepResult", map[string]any{
		"position": w.adapter.runner.Position(),
		"result":   json.RawMessage(resultJSON),
	})
	w.adapter.bufferOrSend(resultEvt, inspect)

	if result.Status == "processed" && inspect && w.adapter.server != nil {
		w.adapter.server.Send(w.adapter.buildStateEvent())
	}
}

func (w *dapEventWriter) OnError(eventID, code, description string) {
	if w.adapter.server != nil {
		w.adapter.server.Send(NewCustomEvent("gaffer/stepError", map[string]any{
			"code":        code,
			"description": description,
		}))
	}
}

// Handler returns the DAP handler callbacks for the server.
func (a *DebugAdapter) Handler() Handler {
	return Handler{
		OnAttach:               a.handleAttach,
		OnSetBreakpoints:       a.handleSetBreakpoints,
		OnContinue:             a.handleContinue,
		OnPause:                a.handlePause,
		OnNext:                 a.handleNext,
		OnStepIn:               a.handleStepIn,
		OnStepOut:              a.handleStepOut,
		OnStackTrace:           a.handleStackTrace,
		OnScopes:               a.handleScopes,
		OnVariables:            a.handleVariables,
		OnEvaluate:             a.handleEvaluate,
		OnConfigurationDone:    a.handleConfigurationDone,
		OnDisconnect:           a.handleDisconnect,
		OnRestart:              a.handleRestart,
		OnGafferGoto:           a.handleGafferGoto,
		OnGafferTimeline:       a.handleGafferTimeline,
		OnGafferPartitionState: a.handleGafferPartitionState,
	}
}

func (a *DebugAdapter) handleSetBreakpoints(s *Server, req *godap.SetBreakpointsRequest) {
	remotePath := a.toRemote(req.Arguments.Source.Path)

	if remotePath != a.sourcePath {
		breakpoints := make([]godap.Breakpoint, len(req.Arguments.Breakpoints))
		for i, bp := range req.Arguments.Breakpoints {
			breakpoints[i] = godap.Breakpoint{
				Id:       i + 1,
				Verified: false,
				Line:     bp.Line,
				Message:  "Not the active projection",
			}
		}
		resp := &godap.SetBreakpointsResponse{}
		resp.Response = NewResponse(req.Seq, req.Command)
		resp.Body.Breakpoints = breakpoints
		s.Send(resp)
		return
	}

	bps := make([]engine.Breakpoint, len(req.Arguments.Breakpoints))
	for i, bp := range req.Arguments.Breakpoints {
		col := bp.Column
		if col == 0 {
			col = 1
		}
		bps[i] = engine.Breakpoint{
			Line:      bp.Line,
			Column:    col,
			Condition: bp.Condition,
		}
	}
	snapped, _ := a.runner.SetBreakpoints(bps)

	a.mu.Lock()
	a.breakpointCount = len(bps)
	a.mu.Unlock()

	breakpoints := make([]godap.Breakpoint, len(req.Arguments.Breakpoints))
	for i := range req.Arguments.Breakpoints {
		if i < len(snapped) && snapped[i] != nil {
			breakpoints[i] = godap.Breakpoint{
				Id:       i + 1,
				Verified: true,
				Line:     snapped[i].Line,
				Column:   snapped[i].Column,
				Source:   a.source(),
			}
		} else {
			breakpoints[i] = godap.Breakpoint{
				Id:       i + 1,
				Verified: false,
				Message:  "No breakable position found",
			}
		}
	}

	resp := &godap.SetBreakpointsResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	resp.Body.Breakpoints = breakpoints
	s.Send(resp)
}

func (a *DebugAdapter) handleContinue(s *Server, req *godap.ContinueRequest) {
	resp := &godap.ContinueResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	resp.Body.AllThreadsContinued = true
	s.Send(resp)

	s.SendEvent(&godap.ContinuedEvent{
		Event: NewEvent("continued"),
		Body:  godap.ContinuedEventBody{ThreadId: 1, AllThreadsContinued: true},
	})

	a.mu.Lock()
	a.inspect = false
	a.mu.Unlock()

	a.server.Send(NewCustomEvent("gaffer/mode", map[string]any{
		"mode": "live",
	}))

	go a.runner.Continue()
}

func (a *DebugAdapter) handlePause(s *Server, req *godap.PauseRequest) {
	a.session.Pause()
	resp := &godap.PauseResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	s.Send(resp)
}

func (a *DebugAdapter) sendStepResponse(s *Server, resp godap.Message, stepFn func()) {
	s.Send(resp)
	s.SendEvent(&godap.ContinuedEvent{
		Event: NewEvent("continued"),
		Body:  godap.ContinuedEventBody{ThreadId: 1, AllThreadsContinued: true},
	})
	go stepFn()
}

func (a *DebugAdapter) handleNext(s *Server, req *godap.NextRequest) {
	resp := &godap.NextResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	a.sendStepResponse(s, resp, a.runner.StepOver)
}

func (a *DebugAdapter) handleStepIn(s *Server, req *godap.StepInRequest) {
	resp := &godap.StepInResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	a.sendStepResponse(s, resp, a.runner.StepInto)
}

func (a *DebugAdapter) handleStepOut(s *Server, req *godap.StepOutRequest) {
	resp := &godap.StepOutResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	a.sendStepResponse(s, resp, a.runner.StepOut)
}

func (a *DebugAdapter) handleStackTrace(s *Server, req *godap.StackTraceRequest) {
	frames, err := a.runner.GetCallStack()
	if err != nil {
		s.Send(NewErrorResponse(req.Seq, req.Command, err.Error()))
		return
	}

	dapFrames := make([]godap.StackFrame, len(frames))
	for i, f := range frames {
		dapFrames[i] = godap.StackFrame{
			Id:     f.ID,
			Name:   f.Name,
			Line:   f.Line,
			Column: f.Column,
			Source: a.source(),
		}
	}

	resp := &godap.StackTraceResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	resp.Body.StackFrames = dapFrames
	resp.Body.TotalFrames = len(dapFrames)
	s.Send(resp)
}

func (a *DebugAdapter) handleScopes(s *Server, req *godap.ScopesRequest) {
	scopes, err := a.runner.GetScopes(req.Arguments.FrameId)
	if err != nil {
		s.Send(NewErrorResponse(req.Seq, req.Command, err.Error()))
		return
	}

	dapScopes := make([]godap.Scope, len(scopes))
	for i, sc := range scopes {
		dapScopes[i] = godap.Scope{
			Name:               sc.Name,
			VariablesReference: sc.VariablesReference,
			Expensive:          sc.Expensive,
		}
	}

	resp := &godap.ScopesResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	resp.Body.Scopes = dapScopes
	s.Send(resp)
}

func (a *DebugAdapter) handleVariables(s *Server, req *godap.VariablesRequest) {
	vars, err := a.runner.GetVariables(req.Arguments.VariablesReference)
	if err != nil {
		s.Send(NewErrorResponse(req.Seq, req.Command, err.Error()))
		return
	}

	dapVars := make([]godap.Variable, len(vars))
	for i, v := range vars {
		dapVars[i] = godap.Variable{
			Name:               v.Name,
			Value:              v.Value,
			Type:               v.Type,
			VariablesReference: v.VariablesReference,
		}
	}

	resp := &godap.VariablesResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	resp.Body.Variables = dapVars
	s.Send(resp)
}

func (a *DebugAdapter) handleEvaluate(s *Server, req *godap.EvaluateRequest) {
	result, err := a.runner.Evaluate(req.Arguments.Expression)
	if err != nil {
		s.Send(NewErrorResponse(req.Seq, req.Command, err.Error()))
		return
	}

	resp := &godap.EvaluateResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	resp.Body.Result = result.Value
	resp.Body.Type = result.Type
	resp.Body.VariablesReference = result.VariablesReference
	s.Send(resp)
}

func (a *DebugAdapter) handleConfigurationDone(s *Server, req *godap.ConfigurationDoneRequest) {
	resp := &godap.ConfigurationDoneResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	s.Send(resp)

	a.mu.Lock()
	pauseAtEntry := a.startPausedIfNoBreakpoints && a.breakpointCount == 0
	if pauseAtEntry {
		a.entryPausePending = true
	}
	a.mu.Unlock()
	if pauseAtEntry {
		a.session.Pause()
	}

	a.mu.Lock()
	a.readyOnce.Do(func() { close(a.readyCh) })
	a.mu.Unlock()
}

// Ready returns a channel that is closed when the editor has completed
// the DAP configuration sequence (configurationDone received). Each
// restart re-arms a fresh channel; callers should re-fetch after
// AckRestart rather than caching the returned value.
func (a *DebugAdapter) Ready() <-chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.readyCh
}

// handleRestart implements DAP's `restart` request (gated on
// SupportsRestartRequest in the InitializeResponse). VS Code sends it
// instead of the disconnect+attach cycle when the user clicks the
// Restart button. We block the read goroutine while dev.go's main
// loop tears down the engine and stands up a fresh one - that way
// the response and the followup Initialized event aren't sent until
// the editor's resent breakpoints will actually land on a live
// session.
func (a *DebugAdapter) handleRestart(s *Server, req *godap.RestartRequest) {
	select {
	case a.restartReqCh <- struct{}{}:
	default:
		// Already pending - dev.go will pick this one up too.
	}
	<-a.restartAckCh

	resp := &godap.RestartResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	s.Send(resp)
	s.Send(&godap.InitializedEvent{Event: NewEvent("initialized")})
}

// RestartRequested returns a channel that receives whenever the editor
// has asked for a restart. dev.go's main loop selects on it to
// interrupt source.Run.
func (a *DebugAdapter) RestartRequested() <-chan struct{} {
	return a.restartReqCh
}

// AckRestart unblocks handleRestart once dev.go has finished tearing
// down the old session and bound a new one. Pairs with the receive
// in handleRestart.
func (a *DebugAdapter) AckRestart() {
	a.restartAckCh <- struct{}{}
}

// ResetForRestart clears per-session adapter state so the next
// configurationDone-driven boot lands cleanly. Called by dev.go's
// main loop between iterations, after the new session/runner are
// bound. The editor's pause-pending UI state lives in
// pause-pending-tracker.ts (driven by stopped/pause DAP wire events)
// and is not mirrored here, so it doesn't need clearing.
func (a *DebugAdapter) ResetForRestart() {
	a.mu.Lock()
	a.inspect = false
	a.entryPausePending = false
	a.lastStats = engine.EventStats{}
	a.lastStateJSON = ""
	a.stepBuffer = nil
	a.breakpointCount = 0
	a.finalStateSent = false
	a.readyOnce = sync.Once{}
	a.readyCh = make(chan struct{})
	a.mu.Unlock()
}

func (a *DebugAdapter) handleDisconnect(s *Server, req *godap.DisconnectRequest) {
	// Send the snapshot before the response: VS Code closes the
	// socket once it sees the DisconnectResponse, so anything we'd
	// have queued from afterRun's SendTerminated never reaches the
	// editor. Final-state has to ride out on this side of the close.
	a.emitFinalState()
	resp := &godap.DisconnectResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	s.Send(resp)
}

func (a *DebugAdapter) bufferOrSend(evt *CustomEvent, inspect bool) {
	if inspect && a.server != nil {
		a.server.Send(evt)
	} else {
		a.mu.Lock()
		a.stepBuffer = append(a.stepBuffer, evt)
		a.mu.Unlock()
	}
}

func (a *DebugAdapter) flushStepBuffer() {
	a.mu.Lock()
	buf := a.stepBuffer
	a.stepBuffer = nil
	a.mu.Unlock()

	if a.server == nil {
		return
	}
	for _, evt := range buf {
		a.server.Send(evt)
	}
	a.server.Send(a.buildStateEvent())
}

func (a *DebugAdapter) buildStateEvent() *CustomEvent {
	summary := a.runner.CollectState()
	body := summary.ToMap()

	// gaffer/state ships partitions as just names; per-partition state
	// is fetched on demand via gaffer/partitionState. gaffer/finalState
	// (emitFinalState) leaves ToMap's keyed object in place so the
	// editor can pre-populate its post-mortem cache - shapes diverge
	// deliberately.
	if summary.Partitioned {
		names := make([]string, 0, len(summary.Partitions))
		for name := range summary.Partitions {
			names = append(names, name)
		}
		body["partitions"] = names
	}

	return NewCustomEvent("gaffer/state", body)
}

// SendTerminated sends a final-state snapshot followed by terminated
// and exited events. The snapshot pre-populates the editor's
// per-partition state cache so post-mortem expansion of partitions
// the user didn't open during the live session shows real values
// rather than "(not loaded during session)".
func (a *DebugAdapter) SendTerminated() {
	if a.server == nil {
		return
	}
	a.emitFinalState()
	a.server.SendEvent(&godap.TerminatedEvent{Event: NewEvent("terminated")})
	a.server.SendEvent(&godap.ExitedEvent{
		Event: NewEvent("exited"),
		Body:  godap.ExitedEventBody{ExitCode: 0},
	})
}

func (a *DebugAdapter) emitFinalState() {
	a.mu.Lock()
	skip := a.finalStateSent || a.runner == nil || a.server == nil
	if !skip {
		a.finalStateSent = true
	}
	a.mu.Unlock()
	if skip {
		return
	}
	defer func() { _ = recover() }()
	summary := a.runner.CollectState()
	a.server.Send(NewCustomEvent("gaffer/finalState", summary.ToMap()))
}

// EmitStatsIfChanged sends a gaffer/stats custom event with cumulative
// counters if anything user-facing has moved since the last emit. Used
// by the dev command's activity ticker to drive the editor's Status
// counter without flooding DAP - per-event step events are buffered
// for the inspect view, so without this the counter would never tick
// during live mode.
//
// Skipped is tracked internally on EventStats but not emitted: skips
// are runtime hygiene noise (link metadata, system deletes, etc.)
// rather than user-actionable events. The change check only looks at
// fields we actually send so a flurry of pure-skip events doesn't
// produce redundant wire traffic.
func (a *DebugAdapter) EmitStatsIfChanged() {
	if a.runner == nil || a.server == nil {
		return
	}
	cur := a.runner.Stats()
	a.mu.Lock()
	if cur.Handled == a.lastStats.Handled && cur.Errors == a.lastStats.Errors {
		a.mu.Unlock()
		return
	}
	a.lastStats = cur
	a.mu.Unlock()
	a.server.Send(NewCustomEvent("gaffer/stats", map[string]any{
		"handled": cur.Handled,
		"errors":  cur.Errors,
	}))
}

// EmitStateIfChanged sends a gaffer/state custom event when the
// projection state has moved since the last emit. Without this, the
// extension's StateProvider would have no data to preserve when the
// user disconnects from a live session - state is otherwise sent
// only when a break flushes the step buffer (inspect mode only).
//
// Wraps the runner read in a recover so a concurrent Destroy on the
// session doesn't take the ticker goroutine down with it; the
// extension already has the previous tick's state to display.
func (a *DebugAdapter) EmitStateIfChanged() {
	if a.runner == nil || a.server == nil {
		return
	}
	defer func() { _ = recover() }()

	evt := a.buildStateEvent()
	body, err := json.Marshal(evt.Body)
	if err != nil {
		return
	}
	a.mu.Lock()
	if string(body) == a.lastStateJSON {
		a.mu.Unlock()
		return
	}
	a.lastStateJSON = string(body)
	a.mu.Unlock()
	a.server.Send(evt)
}

// EmitCaughtUp sends a gaffer/caughtUp DAP event marking the
// subscription as caught up to the live tail.
func (a *DebugAdapter) EmitCaughtUp() {
	if a.server == nil {
		return
	}
	a.server.Send(NewCustomEvent("gaffer/caughtUp", map[string]any{
		"caughtUp": true,
	}))
}

// EmitFellBehind sends a gaffer/caughtUp DAP event marking the
// subscription as having fallen back behind the live tail (the
// reader is scanning chunks again).
func (a *DebugAdapter) EmitFellBehind() {
	if a.server == nil {
		return
	}
	a.server.Send(NewCustomEvent("gaffer/caughtUp", map[string]any{
		"caughtUp": false,
	}))
}

func (a *DebugAdapter) handleGafferGoto(s *Server, req *GafferGotoRequest) {
	step, err := a.runner.GetStep(req.Arguments.Position)
	if err != nil || step == nil {
		s.Send(NewErrorResponse(req.Seq, req.Command, "position not found"))
		return
	}

	body, _ := json.Marshal(map[string]any{
		"position": step.Position,
		"event":    json.RawMessage(step.EventJSON),
		"result":   json.RawMessage(step.ResultJSON),
	})

	resp := &GafferGotoResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	resp.Body = body
	s.Send(resp)
}

func (a *DebugAdapter) handleGafferTimeline(s *Server, req *GafferTimelineRequest) {
	entries, err := a.runner.Timeline(req.Arguments.From, req.Arguments.To)
	if err != nil {
		s.Send(NewErrorResponse(req.Seq, req.Command, err.Error()))
		return
	}

	body, _ := json.Marshal(entries)

	resp := &GafferTimelineResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	resp.Body = body
	s.Send(resp)
}

func (a *DebugAdapter) handleGafferPartitionState(s *Server, req *GafferPartitionStateRequest) {
	partition := req.Arguments.Partition
	body := map[string]any{
		"partition": partition,
	}

	state, result := a.runner.GetPartitionState(partition)
	if state != nil {
		body["state"] = json.RawMessage(*state)
	}
	if result != nil {
		body["result"] = json.RawMessage(*result)
	}

	respBody, _ := json.Marshal(body)
	resp := &GafferPartitionStateResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	resp.Body = respBody
	s.Send(resp)
}

func (a *DebugAdapter) handleAttach(s *Server, req *godap.AttachRequest) {
	var args map[string]any
	if err := json.Unmarshal(req.Arguments, &args); err == nil {
		if lr, ok := args["localRoot"].(string); ok && lr != "" {
			a.localRoot = lr
		}
	}

	resp := &godap.AttachResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	s.Send(resp)
}

func (a *DebugAdapter) source() *godap.Source {
	p := a.toLocal(a.sourcePath)
	return &godap.Source{
		Name: path.Base(p),
		Path: p,
	}
}

func (a *DebugAdapter) toLocal(remotePath string) string {
	if a.localRoot == "" || a.remoteRoot == "" || a.localRoot == a.remoteRoot {
		return remotePath
	}
	return swapPrefix(remotePath, a.remoteRoot, a.localRoot)
}

func (a *DebugAdapter) toRemote(localPath string) string {
	if a.localRoot == "" || a.remoteRoot == "" || a.localRoot == a.remoteRoot {
		return localPath
	}
	return swapPrefix(localPath, a.localRoot, a.remoteRoot)
}

func swapPrefix(p, from, to string) string {
	from = strings.TrimRight(from, "/")
	to = strings.TrimRight(to, "/")
	if p == from {
		return to
	}
	prefix := from + "/"
	if strings.HasPrefix(p, prefix) {
		return to + "/" + p[len(prefix):]
	}
	return p
}
