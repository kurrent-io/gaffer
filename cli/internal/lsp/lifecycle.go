package lsp

import (
	"context"
	"errors"
	"io"

	"github.com/sourcegraph/jsonrpc2"
)

// Run drives the JSON-RPC message loop over the given stream until
// the client closes the connection, sends `exit`, or ctx is
// cancelled.
//
// Returns nil for a clean shutdown (shutdown then exit, or peer
// disconnect before initialize) and a non-nil error for protocol
// violations (initialize without prior shutdown then disconnect or
// exit). Callers map clean shutdown to exit code 0 and protocol
// errors to non-zero.
func (s *Server) Run(ctx context.Context, stream io.ReadWriteCloser) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	conn := jsonrpc2.NewConn(
		runCtx,
		jsonrpc2.NewBufferedStream(stream, jsonrpc2.VSCodeObjectCodec{}),
		jsonrpc2.HandlerWithError(s.handle),
	)
	// Capture the conn + runCtx for server-pushed notifications
	// (publishDiagnostics, registerCapability) and for spawned work
	// (workspace walk) that needs a shutdown signal. Cleared on
	// disconnect so handlers that reach for them post-shutdown bail
	// cleanly.
	s.mu.Lock()
	s.conn = conn
	s.runCtxFn = func() context.Context { return runCtx }
	s.cancelRun = cancel
	s.mu.Unlock()
	defer func() {
		// Teardown order:
		//   1. Cancel runCtx so in-flight parseAndPublish notices
		//      and exits its DescribeBytes call promptly.
		//   2. Drain debouncer timers - any already-queued
		//      callback's identity check finds an empty map and
		//      bails.
		//   3. Set draining under mu - spawn() now refuses new
		//      work, so wg.Add can no longer race wg.Wait.
		//   4. Wait for already-spawned goroutines to settle.
		//   5. Clear captured fields. Goroutines that ran during
		//      steps 1-4 saw the still-live conn/runCtx, which is
		//      what the wg promise gives them.
		cancel()
		s.debouncer.drain()
		s.mu.Lock()
		s.draining = true
		s.mu.Unlock()
		s.wg.Wait()
		s.mu.Lock()
		s.conn = nil
		s.runCtxFn = nil
		s.cancelRun = nil
		s.mu.Unlock()
	}()
	// Three ways out: peer disconnects, client sent exit (we drive
	// disconnect), or our run context was cancelled (e.g. SIGINT).
	// All three converge on closing the connection and waiting for
	// jsonrpc2 to drain in-flight handlers.
	var ctxQuit bool
	select {
	case <-conn.DisconnectNotify():
	case <-s.exitCh:
		_ = conn.Close()
		<-conn.DisconnectNotify()
	case <-ctx.Done():
		ctxQuit = true
		_ = conn.Close()
		<-conn.DisconnectNotify()
	}
	if ctxQuit {
		// Server-side quit (caller cancelled ctx, e.g. SIGINT).
		// Don't blame the client for not sending shutdown - they
		// had no chance.
		return nil
	}
	return s.exitStatus()
}

// snapshotRunState atomically returns the conn and runCtx
// captured by the active Run. Both are nil after Run has begun
// teardown. Handlers that need to spawn work must check before
// using the result.
func (s *Server) snapshotRunState() (*jsonrpc2.Conn, context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runCtxFn == nil {
		return s.conn, nil
	}
	return s.conn, s.runCtxFn()
}

// spawn runs fn in a goroutine tracked by s.wg so Run's defer
// can wait for it to drain before clearing captured fields.
// Returns false (and does not run fn) if Run has begun winding
// down - no-op for callers, since the runCtx they captured is
// already cancelled and any work would terminate immediately.
//
// The draining check + wg.Add(1) MUST happen under s.mu.
// Otherwise a handler could pass snapshotRunState (getting non-
// nil conn/ctx) and call spawn after Run's defer began wg.Wait
// with counter zero - the resulting Add races with Wait.
func (s *Server) spawn(fn func()) bool {
	s.mu.Lock()
	if s.draining {
		s.mu.Unlock()
		return false
	}
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		fn()
	}()
	return true
}

// spawnWithCtx is the canonical helper for handlers that need to
// run async work bounded by runCtx: snapshot the run state, bail
// if Run has wound down, otherwise spawn fn with the captured
// ctx. Folds the snapshot/nil-check/spawn dance into one call.
//
// Returns true when the work was queued.
func (s *Server) spawnWithCtx(fn func(ctx context.Context)) bool {
	_, runCtx := s.snapshotRunState()
	if runCtx == nil {
		return false
	}
	return s.spawn(func() { fn(runCtx) })
}

// signalExit closes exitCh idempotently. Multiple calls are safe -
// only the first close fires; subsequent calls are no-ops.
func (s *Server) signalExit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.exitCh:
		// already closed
	default:
		close(s.exitCh)
	}
}

// exitStatus is consulted at disconnect time to decide whether the
// session ended cleanly. LSP spec: exit without prior shutdown is a
// protocol violation (typically a crashed client).
func (s *Server) exitStatus() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initialized && !s.shutdownReq {
		return errors.New("client disconnected without sending shutdown")
	}
	return nil
}
