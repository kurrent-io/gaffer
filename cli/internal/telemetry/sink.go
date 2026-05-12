package telemetry

import "context"

// Sink is the transport primitive for telemetry envelopes. Implementations
// receive a fully-built Envelope, do whatever they do (POST to a worker,
// write to stderr, buffer for later), and block until done or ctx expires.
//
// The Client passes a freshly-constructed Envelope and drops its own
// reference; the Sink must not retain the Envelope past Send's return
// unless it's making its own copy. The Client never mutates the Envelope
// after Send is called.
//
// Implementations stay synchronous. Goroutine lifecycle, retries, and
// fan-out live in the Client.
type Sink interface {
	Send(ctx context.Context, env *Envelope) error

	// Close releases any sink-held resources (pooled connections, flush
	// buffers, ...). Called by Client.Flush after the WaitGroup drains.
	// Implementations that have nothing to close return nil.
	Close(ctx context.Context) error
}
