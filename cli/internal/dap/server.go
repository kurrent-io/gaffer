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

// Server is a DAP server that bridges editor debug requests to a gaffer session.
type Server struct {
	listener net.Listener
	handler  Handler

	sendCh chan godap.Message
	seq    atomic.Int64
	sendWg sync.WaitGroup

	linesStartAt1   bool
	columnsStartAt1 bool
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
	OnNext                    func(s *Server, req *godap.NextRequest)
	OnStepIn                  func(s *Server, req *godap.StepInRequest)
	OnStepOut                 func(s *Server, req *godap.StepOutRequest)
	OnThreads                 func(s *Server, req *godap.ThreadsRequest)
	OnStackTrace              func(s *Server, req *godap.StackTraceRequest)
	OnScopes                  func(s *Server, req *godap.ScopesRequest)
	OnVariables               func(s *Server, req *godap.VariablesRequest)
	OnEvaluate                func(s *Server, req *godap.EvaluateRequest)
}

// NewServer creates a DAP server listening on the given address.
func NewServer(addr string, handler Handler) (*Server, error) {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dap: listen on %s: %w", addr, err)
	}
	return &Server{
		listener: listener,
		handler:  handler,
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

	s.sendCh = make(chan godap.Message, 100)
	reader := bufio.NewReader(conn)

	s.sendWg.Add(1)
	go s.writeLoop(conn)

	err = s.readLoop(reader)

	close(s.sendCh)
	s.sendWg.Wait()
	s.sendCh = nil
	return err
}

// Close shuts down the listener.
func (s *Server) Close() error {
	return s.listener.Close()
}

// Send queues a message (response or event) to be sent to the client.
func (s *Server) Send(msg godap.Message) {
	if s.sendCh != nil {
		s.sendCh <- msg
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
		msg, err := godap.ReadProtocolMessage(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("dap: read: %w", err)
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
	case *godap.NextRequest:
		if s.handler.OnNext != nil {
			s.handler.OnNext(s, req)
		} else {
			s.Send(NewErrorResponse(req.Seq, req.Command, "not implemented"))
		}
	case *godap.StepInRequest:
		if s.handler.OnStepIn != nil {
			s.handler.OnStepIn(s, req)
		} else {
			s.Send(NewErrorResponse(req.Seq, req.Command, "not implemented"))
		}
	case *godap.StepOutRequest:
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
