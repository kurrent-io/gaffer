package telemetry

import (
	"context"
	"fmt"
	"runtime"
)

// maxStackFrames caps the goroutine stack captured during panic-recover.
// Bigger than any sane gaffer-internal panic; the worker drops oversized
// envelopes anyway, so capping at the source is cheaper than catching it
// at the edge.
const maxStackFrames = 64

// captureStack snapshots the goroutine stack for a panic in progress.
// Pass the result of recover() called directly from the deferred frame;
// captureStack itself cannot call recover() because Go only honours
// recover() when invoked from the immediate deferred function, not from a
// helper called by one.
//
// Returns (nil, nil) when r is nil. When r is non-nil, returns
// (r, structuredFrames) - frames are stdlib runtime.Frame so callers can
// scrub paths / function names / decide in_app independently. The
// exception-emit consumer (commit 11) maps these to the wire Frame type.
//
// Frames are captured before any re-panic so the snapshot doesn't get
// polluted by frames added by the deferred caller.
//
// Tx values must be single-goroutine-owned for the same reason recover()
// is goroutine-local; passing a Tx to a worker goroutine is unsupported.
func captureStack(r any) (panicVal any, frames []runtime.Frame) {
	if r == nil {
		return nil, nil
	}
	pcs := make([]uintptr, maxStackFrames)
	n := runtime.Callers(2, pcs)
	pcs = pcs[:n]
	cf := runtime.CallersFrames(pcs)
	for {
		f, more := cf.Next()
		frames = append(frames, f)
		if !more {
			break
		}
	}
	return r, frames
}

// emit dispatches an envelope to the client's sink on a fresh goroutine
// tracked by the client's WaitGroup. Returns immediately; the actual send
// runs asynchronously. Errors land in the configured error log; the CLI
// never blocks the caller on a failed send.
//
// After Flush has been called, emit is a silent no-op. The lock around
// the close-check-and-Add transition is what prevents wg.Add from racing
// wg.Wait - the atomic-flag-only form was broken because Flush could read
// the counter as 0 in between an emit's Load and its Add.
//
// The Envelope is passed straight through to the Sink; the Client does
// not retain it after returning from emit, and the Sink contract requires
// no post-Send mutation. End() implementations build a fresh Envelope per
// emit and drop their reference.
func (c *Client) emit(env *Envelope) {
	if !c.tryAddInflight() {
		return
	}
	go func() {
		defer c.wg.Done()
		// Recover so a panicking sink (a buggy decorator, a runtime
		// error inside http.Client) doesn't kill the CLI; log and move
		// on - telemetry is best-effort.
		defer func() {
			if r := recover(); r != nil {
				c.errLog(fmt.Errorf("gaffer telemetry: sink panicked: %v", r))
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), c.perSendTimeout)
		defer cancel()
		if err := c.sink.Send(ctx, env); err != nil {
			c.errLog(fmt.Errorf("gaffer telemetry: send failed: %w", err))
		}
	}()
}

// tryAddInflight increments the in-flight WaitGroup if the client isn't
// closed yet. Returns false if closed (caller should drop the work). The
// Add happens inside the same mutex that Flush uses to set closed - if
// tryAddInflight returns true, the eventual wg.Done is guaranteed to be
// observed by Flush's wg.Wait (no Add-after-Wait window).
func (c *Client) tryAddInflight() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.wg.Add(1)
	return true
}

// Flush waits for all in-flight sends to complete or for ctx to expire.
// Returns nil on clean drain, a wrapped ctx.Err() on timeout.
//
// Idempotent. First call closes the client (subsequent emits become
// no-ops); later calls wait again on whatever's outstanding (typically
// nothing) and return promptly. The Client is single-use post-Flush -
// don't emit again after Flush returns and expect to see anything.
//
// Call once at process exit, after cmd.Execute() returns and before
// os.Exit. Bound the wait with context.WithTimeout so a stalled worker
// can't keep the process alive; a value at least as large as the per-send
// timeout (default 2 seconds) lets the per-send budget actually elapse.
func (c *Client) Flush(ctx context.Context) error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()

	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	var err error
	select {
	case <-done:
	case <-ctx.Done():
		err = fmt.Errorf("telemetry flush: %w", ctx.Err())
	}
	// Close the sink whether or not we drained cleanly. In-flight
	// goroutines (if any) hold their own references and finish on their
	// per-send budget; the OS will reap their fds when the process
	// exits regardless. Close lets a sink that has its own resources
	// (a future buffered sink, the http transport's idle pool) release
	// them while we're still in user code.
	if closeErr := c.sink.Close(ctx); closeErr != nil && err == nil {
		err = fmt.Errorf("telemetry sink close: %w", closeErr)
	}
	return err
}
