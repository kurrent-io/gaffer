package dap

import (
	"encoding/json"
	"path"
	"strings"
	"sync"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/history"

	godap "github.com/google/go-dap"
)

// ProjectionShape describes the projection's features for the adapter.
type ProjectionShape struct {
	IsPartitioned   bool
	IsBiState       bool
	HasTransforms   bool
	ProducesResults bool
}

// DebugAdapter bridges a DAP server to a gaffer runtime session.
type DebugAdapter struct {
	session    *gafferruntime.Session
	server     *Server
	history    *history.Store
	shape      ProjectionShape
	sourcePath string
	remoteRoot string
	localRoot  string

	mu         sync.Mutex
	paused     bool
	partitions []string
	partSet    map[string]bool

	readyOnce sync.Once
	readyCh   chan struct{}
}

// NewDebugAdapter creates an adapter that wires a DAP server to a session.
// sourcePath is the filesystem path to the projection JS file (for Source objects).
// remoteRoot is the project root (where gaffer.toml lives) on the server side.
// Call SetServer before starting the server.
func NewDebugAdapter(session *gafferruntime.Session, sourcePath, remoteRoot string, store *history.Store, shape ProjectionShape) *DebugAdapter {
	return &DebugAdapter{
		session:    session,
		sourcePath: sourcePath,
		remoteRoot: remoteRoot,
		history:    store,
		shape:      shape,
		readyCh:    make(chan struct{}),
	}
}

// SetServer connects the adapter to a DAP server for sending events.
// Also wires up session callbacks (OnBreak, OnLog, OnEmit).
func (a *DebugAdapter) SetServer(server *Server) {
	a.server = server

	a.session.OnBreak(func(info gafferruntime.BreakInfo) {
		a.mu.Lock()
		a.paused = true
		a.mu.Unlock()

		reason := info.Reason
		if reason == "debugger_statement" {
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
	})

	a.session.OnLog(func(message string) {
		a.server.SendEvent(&godap.OutputEvent{
			Event: NewEvent("output"),
			Body: godap.OutputEventBody{
				Category: "console",
				Output:   message + "\n",
			},
		})
		a.server.Send(NewCustomEvent("gaffer/stepLog", map[string]any{
			"message": message,
		}))
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
		a.server.Send(NewCustomEvent("gaffer/stepEmit", body))
	})
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

	a.session.ClearBreakpoints()

	breakpoints := make([]godap.Breakpoint, len(req.Arguments.Breakpoints))
	for i, bp := range req.Arguments.Breakpoints {
		col := bp.Column
		if col == 0 {
			col = 1
		}
		var opts *gafferruntime.BreakpointOptions
		if bp.Condition != "" || bp.HitCondition != "" || bp.LogMessage != "" {
			opts = &gafferruntime.BreakpointOptions{
				Condition:    bp.Condition,
				HitCondition: bp.HitCondition,
				LogMessage:   bp.LogMessage,
			}
		}
		snapped, _ := a.session.SetBreakpoint(bp.Line, col, opts)
		if snapped != nil {
			breakpoints[i] = godap.Breakpoint{
				Id:       i + 1,
				Verified: true,
				Line:     snapped.Line,
				Column:   snapped.Column,
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
	a.paused = false
	a.mu.Unlock()

	go a.session.Continue()
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
	a.mu.Lock()
	a.paused = false
	a.mu.Unlock()
	go stepFn()
}

func (a *DebugAdapter) handleNext(s *Server, req *godap.NextRequest) {
	resp := &godap.NextResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	a.sendStepResponse(s, resp, a.session.StepOver)
}

func (a *DebugAdapter) handleStepIn(s *Server, req *godap.StepInRequest) {
	resp := &godap.StepInResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	a.sendStepResponse(s, resp, a.session.StepInto)
}

func (a *DebugAdapter) handleStepOut(s *Server, req *godap.StepOutRequest) {
	resp := &godap.StepOutResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	a.sendStepResponse(s, resp, a.session.StepOut)
}

func (a *DebugAdapter) handleStackTrace(s *Server, req *godap.StackTraceRequest) {
	frames, err := a.session.GetCallStack()
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
	scopes, err := a.session.GetScopes(req.Arguments.FrameId)
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
	vars, err := a.session.GetVariables(req.Arguments.VariablesReference)
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
	result, err := a.session.Evaluate(req.Arguments.Expression)
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
	a.readyOnce.Do(func() { close(a.readyCh) })
}

// Ready returns a channel that is closed when the editor has completed
// the DAP configuration sequence (configurationDone received).
func (a *DebugAdapter) Ready() <-chan struct{} {
	return a.readyCh
}

func (a *DebugAdapter) handleDisconnect(s *Server, req *godap.DisconnectRequest) {
	resp := &godap.DisconnectResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	s.Send(resp)
}

// FeedEvent feeds a single event, records it in history, sends incremental
// DAP events (stepStart -> callbacks -> stepResult), and returns the result.
func (a *DebugAdapter) FeedEvent(eventJSON string) (*gafferruntime.FeedResult, error) {
	if a.server != nil {
		a.server.Send(NewCustomEvent("gaffer/stepStart", map[string]any{
			"event": json.RawMessage(eventJSON),
		}))
	}

	result, err := a.session.Feed(eventJSON)
	if err != nil {
		if a.server != nil {
			code := "unexpected-error"
			description := err.Error()
			if projErr, ok := err.(gafferruntime.ProjectionError); ok {
				code = projErr.ErrorCode()
				description = projErr.ErrorDescription()
			}
			a.server.Send(NewCustomEvent("gaffer/stepError", map[string]any{
				"code":        code,
				"description": description,
			}))
		}
		return nil, err
	}

	resultJSON, _ := json.Marshal(result)

	var position int64
	if a.history != nil {
		position, _ = a.history.Insert(eventJSON, string(resultJSON))
	}

	if result.Status == "processed" && result.Partition != "" {
		a.mu.Lock()
		if a.partSet == nil {
			a.partSet = make(map[string]bool)
		}
		if !a.partSet[result.Partition] {
			a.partSet[result.Partition] = true
			a.partitions = append(a.partitions, result.Partition)
		}
		a.mu.Unlock()
	}

	if a.server != nil {
		a.server.Send(NewCustomEvent("gaffer/stepResult", map[string]any{
			"position": position,
			"result":   json.RawMessage(resultJSON),
		}))

		if result.Status == "processed" {
			a.sendStateEvent()
		}
	}

	return result, nil
}

func (a *DebugAdapter) sendStateEvent() {
	body := map[string]any{}

	a.mu.Lock()
	hasPartitions := len(a.partitions) > 0
	var partitionsCopy []string
	if hasPartitions {
		partitionsCopy = make([]string, len(a.partitions))
		copy(partitionsCopy, a.partitions)
	}
	a.mu.Unlock()

	if hasPartitions {
		body["partitions"] = partitionsCopy
	} else {
		if state := a.session.GetState(nil); state != nil {
			body["state"] = json.RawMessage(*state)
		}
		if a.shape.ProducesResults {
			if result, err := a.session.GetResult(nil); err == nil && result != nil {
				body["result"] = json.RawMessage(*result)
			}
		}
	}

	if shared := a.session.GetSharedState(); shared != nil {
		body["sharedState"] = json.RawMessage(*shared)
	}

	a.server.Send(NewCustomEvent("gaffer/state", body))
}

// SendTerminated sends terminated and exited events to the editor.
func (a *DebugAdapter) SendTerminated() {
	if a.server == nil {
		return
	}
	a.server.SendEvent(&godap.TerminatedEvent{Event: NewEvent("terminated")})
	a.server.SendEvent(&godap.ExitedEvent{
		Event: NewEvent("exited"),
		Body:  godap.ExitedEventBody{ExitCode: 0},
	})
}

func (a *DebugAdapter) handleGafferGoto(s *Server, req *GafferGotoRequest) {
	if a.history == nil {
		s.Send(NewErrorResponse(req.Seq, req.Command, "no history available"))
		return
	}

	step, err := a.history.Get(req.Arguments.Position)
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
	if a.history == nil {
		s.Send(NewErrorResponse(req.Seq, req.Command, "no history available"))
		return
	}

	entries, err := a.history.Timeline(req.Arguments.From, req.Arguments.To)
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

	if state := a.session.GetState(&partition); state != nil {
		body["state"] = json.RawMessage(*state)
	}
	if a.shape.ProducesResults {
		if result, err := a.session.GetResult(&partition); err == nil && result != nil {
			body["result"] = json.RawMessage(*result)
		}
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
