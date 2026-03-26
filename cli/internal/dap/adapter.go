package dap

import (
	"path"
	"sync"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"

	godap "github.com/google/go-dap"
)

// DebugAdapter bridges a DAP server to a gaffer runtime session.
type DebugAdapter struct {
	session    *gafferruntime.Session
	server     *Server
	sourcePath string

	mu     sync.Mutex
	paused bool

	readyOnce sync.Once
	readyCh   chan struct{}
}

// NewDebugAdapter creates an adapter that wires a DAP server to a session.
// sourcePath is the filesystem path to the projection JS file (for Source objects).
// Call SetServer before starting the server.
func NewDebugAdapter(session *gafferruntime.Session, sourcePath string) *DebugAdapter {
	return &DebugAdapter{
		session:    session,
		sourcePath: sourcePath,
		readyCh:    make(chan struct{}),
	}
}

// SetServer connects the adapter to a DAP server for sending events.
// Also wires up session callbacks (OnBreak, OnLog).
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
	})
}

// Handler returns the DAP handler callbacks for the server.
func (a *DebugAdapter) Handler() Handler {
	return Handler{
		OnSetBreakpoints:    a.handleSetBreakpoints,
		OnContinue:          a.handleContinue,
		OnStackTrace:        a.handleStackTrace,
		OnScopes:            a.handleScopes,
		OnVariables:         a.handleVariables,
		OnConfigurationDone: a.handleConfigurationDone,
		OnDisconnect:        a.handleDisconnect,
	}
}

func (a *DebugAdapter) handleSetBreakpoints(s *Server, req *godap.SetBreakpointsRequest) {
	reqFile := path.Base(req.Arguments.Source.Path)
	projFile := path.Base(a.sourcePath)

	// Only accept breakpoints for the projection being debugged
	if reqFile != projFile {
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

	// Capture the editor's source path for stack frames (handles container path mismatch)
	if req.Arguments.Source.Path != "" {
		a.sourcePath = req.Arguments.Source.Path
	}
	a.session.ClearBreakpoints()

	breakpoints := make([]godap.Breakpoint, len(req.Arguments.Breakpoints))
	for i, bp := range req.Arguments.Breakpoints {
		col := bp.Column
		if col == 0 {
			col = 1
		}
		snapped, _ := a.session.SetBreakpoint(bp.Line, col)
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

// FeedEvent feeds a single event and returns the result.
// Blocks until the event is processed (including debug pauses).
func (a *DebugAdapter) FeedEvent(eventJSON string) (*gafferruntime.FeedResult, error) {
	return a.session.Feed(eventJSON)
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

func (a *DebugAdapter) source() *godap.Source {
	name := path.Base(a.sourcePath)
	return &godap.Source{
		Name: name,
		Path: a.sourcePath,
	}
}
