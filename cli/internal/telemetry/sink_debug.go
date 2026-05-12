package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// debugTeeSink wraps an inner Sink, writing every envelope as JSON to
// w before forwarding to the inner sink. Wired in when
// GAFFER_TELEMETRY_DEBUG=1 is set so users can audit exactly what
// would be sent (critical for the "show me what you collect" trust
// posture promised in TELEMETRY.md).
//
// Failed writes to the debug writer are reported via errLog so a
// caller test or future structured-log consumer can observe them; we
// don't return the error from Send because the inner sink still gets
// to try its own send.
//
// Note on double-logging: on a marshal failure, the tee reports the
// error via errLog AND forwards to the inner sink. httpSink will
// also try to marshal the same envelope and fail, producing a second
// errLog entry for the same envelope. Acceptable - marshal failure
// is essentially impossible for the generated Envelope shape (no
// chan / func fields), so the duplication isn't a real cost.
type debugTeeSink struct {
	inner Sink
	// writeMu serialises writes to w. emit() spawns a goroutine per
	// envelope, so multiple Send calls run concurrently; POSIX only
	// guarantees writes <=PIPE_BUF (4KB) are atomic, and a
	// projection_shape envelope easily exceeds that. Without the
	// mutex, large envelopes could split and interleave on stderr.
	writeMu sync.Mutex
	w       io.Writer
	errLog  func(error)
}

// newDebugTeeSink wraps inner. errLog may be nil; the constructor
// replaces it with a no-op so Send doesn't have to nil-check on every
// call.
func newDebugTeeSink(inner Sink, w io.Writer, errLog func(error)) *debugTeeSink {
	if errLog == nil {
		errLog = func(error) {}
	}
	return &debugTeeSink{inner: inner, w: w, errLog: errLog}
}

func (d *debugTeeSink) Send(ctx context.Context, env *Envelope) error {
	body, err := json.Marshal(env)
	if err != nil {
		d.errLog(fmt.Errorf("gaffer telemetry: debug-tee marshal: %w", err))
	} else {
		d.writeMu.Lock()
		_, werr := fmt.Fprintf(d.w, "gaffer-telemetry: %s\n", body)
		d.writeMu.Unlock()
		if werr != nil {
			d.errLog(fmt.Errorf("gaffer telemetry: debug-tee write: %w", werr))
		}
	}
	return d.inner.Send(ctx, env)
}

func (d *debugTeeSink) Close(ctx context.Context) error {
	return d.inner.Close(ctx)
}
