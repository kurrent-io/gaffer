package telemetry

import (
	"context"
	"sync"
	"time"
)

// internalMockSink is a test-only Sink used by tests inside package
// telemetry. The public MockSink (telemetrytest.MockSink) imports
// telemetry, so it can't be used here without an import cycle. Shape
// mirrors MockSink so the two stay in sync; the duplication is small.
type internalMockSink struct {
	mu      sync.Mutex
	envs    []*Envelope
	sendErr error
	delay   time.Duration
}

func newMockSink() *internalMockSink { return &internalMockSink{} }

func (s *internalMockSink) Send(ctx context.Context, env *Envelope) error {
	if d := s.getDelay(); d > 0 {
		select {
		case <-time.After(d):
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

func (s *internalMockSink) Close(context.Context) error { return nil }

func (s *internalMockSink) SetSendErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sendErr = err
}

func (s *internalMockSink) SetDelay(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delay = d
}

func (s *internalMockSink) getDelay() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.delay
}

func (s *internalMockSink) Envelopes() []*Envelope {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Envelope, len(s.envs))
	copy(out, s.envs)
	return out
}

func (s *internalMockSink) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.envs)
}
