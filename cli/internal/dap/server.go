// Package dap implements a Debug Adapter Protocol server for gaffer projections.
package dap

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"sync/atomic"

	godap "github.com/google/go-dap"
)

// Stats is the typed counter snapshot the cobra RunE drains at
// tx.End() time. Lives in dap (not telemetry) so the server stays
// free of telemetry imports; the translation to typed DebugTx
// setters happens at the cobra layer.
//
// Counters record attempts (every dispatch of the corresponding
// request type), not "user actions" - SetBreakpointsRequest
// arrives once per editor breakpoint pane change, so the
// BreakpointCount tracks editor activity, not the runtime
// hit-counts. StepCount aggregates next + step-in + step-out
// (the wire schema doesn't break them apart and editor UX
// treats them as one category).
type Stats struct {
	BreakpointCount int
	StepCount       int
	PauseCount      int
	RestartCount    int
	// ProtocolError is the non-EOF error from the read loop, or nil
	// if the editor disconnected cleanly. Stats() copies it from the
	// Server's stored value (set once by Serve on return); the cobra
	// wrapper maps non-nil to outcome=dap_protocol_error.
	//
	// Sitting alongside the counters here rather than a separate
	// accessor: the cobra wrapper drains everything at the same End
	// time and threading two values out (Stats + ProtocolError) via
	// independent out-params bloats the runDev signature more than
	// this field's "errors aren't stats" smell costs.
	ProtocolError error
}

// serverStats holds the in-flight counters bumped by the dispatch
// loop before each handler invocation. Atomics for goroutine
// safety; reads via Stats() see the final values after Serve
// returns.
type serverStats struct {
	breakpoints atomic.Int64
	steps       atomic.Int64
	pauses      atomic.Int64
	restarts    atomic.Int64
}

// Server is a DAP server that bridges editor debug requests to a gaffer session.
type Server struct {
	listener net.Listener
	handler  Handler
	codec    *godap.Codec

	// sendMu guards sendCh + sendOpen. Send may be called from any
	// goroutine (engine source loop, DAP handler, runtime callbacks)
	// while Serve owns the open/close lifecycle. Without the mutex,
	// the field write from `s.sendCh = make(...)` races the read in
	// Send, and a Send racing the close panics "send on closed
	// channel". writeLoop's `for msg := range s.sendCh` reads the
	// channel via the value captured at goroutine launch, after the
	// open assignment - safe without the mutex because sendWg.Wait
	// gates the close on writeLoop's exit.
	sendMu   sync.Mutex
	sendCh   chan godap.Message
	sendOpen bool
	seq      atomic.Int64
	sendWg   sync.WaitGroup

	linesStartAt1   bool
	columnsStartAt1 bool

	stats serverStats

	// protocolErr captures the non-EOF error from readLoop. Set once
	// by Serve, read by ProtocolError() at telemetry End time so the
	// cobra wrapper can distinguish "editor disconnected normally"
	// (EOF -> nil) from "protocol-level read or decode failure"
	// (which maps to outcome=dap_protocol_error). atomic.Value lets
	// the End-time read happen after Serve returned without an
	// explicit happens-before from the writer.
	protocolErr atomic.Value // holds error
}

// Stats returns the current counter snapshot. Safe to call from
// any goroutine; the cobra RunE for `gaffer dev --debug` reads
// this at tx.End() time after Serve has returned.
func (s *Server) Stats() Stats {
	var protoErr error
	if v := s.protocolErr.Load(); v != nil {
		protoErr = v.(error)
	}
	return Stats{
		BreakpointCount: int(s.stats.breakpoints.Load()),
		StepCount:       int(s.stats.steps.Load()),
		PauseCount:      int(s.stats.pauses.Load()),
		RestartCount:    int(s.stats.restarts.Load()),
		ProtocolError:   protoErr,
	}
}

// Handler contains callbacks for each DAP request type.
type Handler struct {
	OnInitialize              func(s *Server, req *godap.InitializeRequest)
	OnAttach                  func(s *Server, req *godap.AttachRequest)
	OnConfigurationDone       func(s *Server, req *godap.ConfigurationDoneRequest)
	OnDisconnect              func(s *Server, req *godap.DisconnectRequest)
	OnSetBreakpoints          func(s *Server, req *godap.SetBreakpointsRequest)
	OnSetExceptionBreakpoints func(s *Server, req *godap.SetExceptionBreakpointsRequest)
	OnContinue                func(s *Server, req *godap.ContinueRequest)
	OnPause                   func(s *Server, req *godap.PauseRequest)
	OnRestart                 func(s *Server, req *godap.RestartRequest)
	OnNext                    func(s *Server, req *godap.NextRequest)
	OnStepIn                  func(s *Server, req *godap.StepInRequest)
	OnStepOut                 func(s *Server, req *godap.StepOutRequest)
	OnThreads                 func(s *Server, req *godap.ThreadsRequest)
	OnStackTrace              func(s *Server, req *godap.StackTraceRequest)
	OnScopes                  func(s *Server, req *godap.ScopesRequest)
	OnVariables               func(s *Server, req *godap.VariablesRequest)
	OnEvaluate                func(s *Server, req *godap.EvaluateRequest)
	OnGafferGoto              func(s *Server, req *GafferGotoRequest)
	OnGafferTimeline          func(s *Server, req *GafferTimelineRequest)
	OnGafferPartitionState    func(s *Server, req *GafferPartitionStateRequest)
}

// NewServer creates a DAP server listening on the given address.
func NewServer(addr string, handler Handler) (*Server, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dap: listen on %s: %w", addr, err)
	}
	codec := godap.NewCodec()
	RegisterCustomRequests(codec)

	return &Server{
		listener: listener,
		handler:  handler,
		codec:    codec,
	}, nil
}

// Addr returns the address the server is listening on.
func (s *Server) Addr() net.Addr {
	return s.listener.Addr()
}

// Serve accepts a single connection and handles it. Blocks until the
// connection is closed or an error occurs. Call again to accept another
// connection (reconnect support).
func (s *Server) Serve() error {
	conn, err := s.listener.Accept()
	if err != nil {
		return fmt.Errorf("dap: accept: %w", err)
	}
	defer func() { _ = conn.Close() }()

	s.sendMu.Lock()
	s.sendCh = make(chan godap.Message, 100)
	s.sendOpen = true
	s.sendMu.Unlock()

	reader := bufio.NewReader(conn)

	s.sendWg.Add(1)
	go s.writeLoop(conn)

	err = s.readLoop(reader)
	if err != nil {
		s.protocolErr.Store(err)
	}

	// Mark closed before closing the channel so any racing Send sees
	// !sendOpen and bails before attempting a panicking send.
	s.sendMu.Lock()
	s.sendOpen = false
	close(s.sendCh)
	s.sendMu.Unlock()

	s.sendWg.Wait()
	return err
}

// Close shuts down the listener.
func (s *Server) Close() error {
	return s.listener.Close()
}

// Send queues a message (response or event) to be sent to the client.
// Drop-on-full: a wedged buffer means the editor has stopped reading
// (broken connection); blocking here would just stall the engine
// behind a doomed write. Drops are logged.
func (s *Server) Send(msg godap.Message) {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	if !s.sendOpen {
		return
	}
	select {
	case s.sendCh <- msg:
	default:
		log.Printf("dap: send buffer full (%d/%d), dropping %T", len(s.sendCh), cap(s.sendCh), msg)
	}
}

// SendEvent sends a DAP event to the client.
func (s *Server) SendEvent(event godap.EventMessage) {
	s.Send(event)
}

// NewResponse creates a success response for the given request.
func NewResponse(requestSeq int, command string) godap.Response {
	return godap.Response{
		ProtocolMessage: godap.ProtocolMessage{Type: "response"},
		RequestSeq:      requestSeq,
		Command:         command,
		Success:         true,
	}
}

// NewErrorResponse creates an error response.
func NewErrorResponse(requestSeq int, command string, message string) *godap.ErrorResponse {
	r := &godap.ErrorResponse{}
	r.Response = NewResponse(requestSeq, command)
	r.Success = false
	r.Message = message
	return r
}

// NewEvent creates a DAP event with the given type.
func NewEvent(event string) godap.Event {
	return godap.Event{
		ProtocolMessage: godap.ProtocolMessage{Type: "event"},
		Event:           event,
	}
}

func (s *Server) readLoop(reader *bufio.Reader) error {
	for {
		data, err := godap.ReadBaseMessage(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("dap: read: %w", err)
		}
		msg, err := s.codec.DecodeMessage(data)
		if err != nil {
			log.Printf("dap: decode error: %v", err)
			continue
		}
		s.dispatch(msg)
	}
}

func (s *Server) writeLoop(conn net.Conn) {
	defer s.sendWg.Done()
	writer := bufio.NewWriter(conn)
	for msg := range s.sendCh {
		switch m := msg.(type) {
		case godap.ResponseMessage:
			if resp := m.GetResponse(); resp.Seq == 0 {
				resp.Seq = int(s.seq.Add(1))
			}
		case godap.EventMessage:
			if event := m.GetEvent(); event.Seq == 0 {
				event.Seq = int(s.seq.Add(1))
			}
		}
		if err := godap.WriteProtocolMessage(writer, msg); err != nil {
			log.Printf("dap: write error: %v", err)
			return
		}
		_ = writer.Flush()
	}
}

func (s *Server) dispatch(msg godap.Message) {
	switch req := msg.(type) {
	case *godap.InitializeRequest:
		s.handleInitialize(req)
	case *godap.AttachRequest:
		if s.handler.OnAttach != nil {
			s.handler.OnAttach(s, req)
		} else {
			s.Send(s.defaultAttachResponse(req))
		}
	case *godap.ConfigurationDoneRequest:
		if s.handler.OnConfigurationDone != nil {
			s.handler.OnConfigurationDone(s, req)
		} else {
			resp := &godap.ConfigurationDoneResponse{}
			resp.Response = NewResponse(req.Seq, req.Command)
			s.Send(resp)
		}
	case *godap.DisconnectRequest:
		if s.handler.OnDisconnect != nil {
			s.handler.OnDisconnect(s, req)
		} else {
			resp := &godap.DisconnectResponse{}
			resp.Response = NewResponse(req.Seq, req.Command)
			s.Send(resp)
		}
	case *godap.SetBreakpointsRequest:
		s.stats.breakpoints.Add(1)
		if s.handler.OnSetBreakpoints != nil {
			s.handler.OnSetBreakpoints(s, req)
		} else {
			s.Send(NewErrorResponse(req.Seq, req.Command, "not implemented"))
		}
	case *godap.SetExceptionBreakpointsRequest:
		if s.handler.OnSetExceptionBreakpoints != nil {
			s.handler.OnSetExceptionBreakpoints(s, req)
		} else {
			resp := &godap.SetExceptionBreakpointsResponse{}
			resp.Response = NewResponse(req.Seq, req.Command)
			s.Send(resp)
		}
	case *godap.ContinueRequest:
		if s.handler.OnContinue != nil {
			s.handler.OnContinue(s, req)
		} else {
			s.Send(NewErrorResponse(req.Seq, req.Command, "not implemented"))
		}
	case *godap.PauseRequest:
		s.stats.pauses.Add(1)
		if s.handler.OnPause != nil {
			s.handler.OnPause(s, req)
		} else {
			s.Send(NewErrorResponse(req.Seq, req.Command, "not implemented"))
		}
	case *godap.RestartRequest:
		s.stats.restarts.Add(1)
		if s.handler.OnRestart != nil {
			s.handler.OnRestart(s, req)
		} else {
			s.Send(NewErrorResponse(req.Seq, req.Command, "not implemented"))
		}
	case *godap.NextRequest:
		s.stats.steps.Add(1)
		if s.handler.OnNext != nil {
			s.handler.OnNext(s, req)
		} else {
			s.Send(NewErrorResponse(req.Seq, req.Command, "not implemented"))
		}
	case *godap.StepInRequest:
		s.stats.steps.Add(1)
		if s.handler.OnStepIn != nil {
			s.handler.OnStepIn(s, req)
		} else {
			s.Send(NewErrorResponse(req.Seq, req.Command, "not implemented"))
		}
	case *godap.StepOutRequest:
		s.stats.steps.Add(1)
		if s.handler.OnStepOut != nil {
			s.handler.OnStepOut(s, req)
		} else {
			s.Send(NewErrorResponse(req.Seq, req.Command, "not implemented"))
		}
	case *godap.ThreadsRequest:
		if s.handler.OnThreads != nil {
			s.handler.OnThreads(s, req)
		} else {
			s.Send(s.defaultThreadsResponse(req))
		}
	case *godap.StackTraceRequest:
		if s.handler.OnStackTrace != nil {
			s.handler.OnStackTrace(s, req)
		} else {
			s.Send(NewErrorResponse(req.Seq, req.Command, "not implemented"))
		}
	case *godap.ScopesRequest:
		if s.handler.OnScopes != nil {
			s.handler.OnScopes(s, req)
		} else {
			s.Send(NewErrorResponse(req.Seq, req.Command, "not implemented"))
		}
	case *godap.VariablesRequest:
		if s.handler.OnVariables != nil {
			s.handler.OnVariables(s, req)
		} else {
			s.Send(NewErrorResponse(req.Seq, req.Command, "not implemented"))
		}
	case *godap.EvaluateRequest:
		if s.handler.OnEvaluate != nil {
			s.handler.OnEvaluate(s, req)
		} else {
			s.Send(NewErrorResponse(req.Seq, req.Command, "not implemented"))
		}
	case *GafferGotoRequest:
		if s.handler.OnGafferGoto != nil {
			s.handler.OnGafferGoto(s, req)
		} else {
			s.Send(NewErrorResponse(req.Seq, req.Command, "not implemented"))
		}
	case *GafferTimelineRequest:
		if s.handler.OnGafferTimeline != nil {
			s.handler.OnGafferTimeline(s, req)
		} else {
			s.Send(NewErrorResponse(req.Seq, req.Command, "not implemented"))
		}
	case *GafferPartitionStateRequest:
		if s.handler.OnGafferPartitionState != nil {
			s.handler.OnGafferPartitionState(s, req)
		} else {
			s.Send(NewErrorResponse(req.Seq, req.Command, "not implemented"))
		}
	default:
		if req, ok := msg.(godap.RequestMessage); ok {
			s.Send(NewErrorResponse(req.GetSeq(), req.GetRequest().Command, "not supported"))
		}
	}
}

func (s *Server) handleInitialize(req *godap.InitializeRequest) {
	s.linesStartAt1 = req.Arguments.LinesStartAt1
	s.columnsStartAt1 = req.Arguments.ColumnsStartAt1

	if s.handler.OnInitialize != nil {
		s.handler.OnInitialize(s, req)
		return
	}

	resp := &godap.InitializeResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	resp.Body.SupportsConfigurationDoneRequest = true
	resp.Body.SupportsConditionalBreakpoints = true
	resp.Body.SupportsHitConditionalBreakpoints = true
	resp.Body.SupportsLogPoints = true
	resp.Body.SupportsRestartRequest = true
	s.Send(resp)

	s.Send(&godap.InitializedEvent{Event: NewEvent("initialized")})
}

func (s *Server) defaultAttachResponse(req *godap.AttachRequest) *godap.AttachResponse {
	resp := &godap.AttachResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	return resp
}

func (s *Server) defaultThreadsResponse(req *godap.ThreadsRequest) *godap.ThreadsResponse {
	resp := &godap.ThreadsResponse{}
	resp.Response = NewResponse(req.Seq, req.Command)
	resp.Body.Threads = []godap.Thread{{Id: 1, Name: "projection"}}
	return resp
}
