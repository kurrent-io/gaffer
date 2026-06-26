// Package telemetrytest holds test helpers for the telemetry package and
// its consumers. Imported from cli/cmd/*_test.go integration tests that
// inject a MockSink via telemetry.WithSink(...) to assert on what would be
// emitted without making real HTTP calls.
package telemetrytest

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
)

// MockSink is a Sink that records envelopes in memory and optionally
// injects errors or delays. Methods are safe for concurrent use.
//
// Callers should not mutate Envelopes after passing them to Send; the
// telemetry.Sink contract documents that, and MockSink stores the pointer
// directly so a post-Send mutation would race a reader.
type MockSink struct {
	mu      sync.Mutex
	envs    []*telemetry.Envelope
	sendErr error
	delay   time.Duration
}

// New constructs an empty MockSink.
func New() *MockSink { return &MockSink{} }

// Send implements telemetry.Sink. Respects ctx during the configured
// delay; the configured error (if any) is returned ignoring ctx.
func (s *MockSink) Send(ctx context.Context, env *telemetry.Envelope) error {
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sendErr != nil {
		return s.sendErr
	}
	s.envs = append(s.envs, env)
	return nil
}

// Close implements telemetry.Sink. No-op.
func (s *MockSink) Close(context.Context) error { return nil }

// SetSendErr makes future Send calls return err.
func (s *MockSink) SetSendErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendErr = err
}

// SetDelay makes Send sleep before recording. The sleep respects the ctx
// passed to Send.
func (s *MockSink) SetDelay(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delay = d
}

// Envelopes returns a copy of the recorded envelope slice. The Envelopes
// themselves are returned by pointer; per the Sink contract callers won't
// have mutated them, so they're safe to inspect.
func (s *MockSink) Envelopes() []*telemetry.Envelope {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.envs)
}

// Len returns the number of envelopes recorded. Cheaper than Envelopes()
// for tests that only check a count.
func (s *MockSink) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.envs)
}

// Reset drops the recorded envelopes (and clears any configured error /
// delay). Lets a single MockSink span multiple test cases.
func (s *MockSink) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.envs = nil
	s.sendErr = nil
	s.delay = 0
}
