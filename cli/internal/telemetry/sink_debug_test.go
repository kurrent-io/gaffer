package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDebugTeeSink_WritesAndForwards(t *testing.T) {
	var buf bytes.Buffer
	inner := newMockSink()
	d := newDebugTeeSink(inner, &buf, nil)
	env := &Envelope{SchemaVersion: EnvelopeSchemaVersion1, EmitterID: "tee-test"}
	if err := d.Send(context.Background(), env); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := inner.Len(); got != 1 {
		t.Errorf("inner.Len = %d, want 1", got)
	}

	out := buf.String()
	if !strings.HasPrefix(out, "gaffer-telemetry: ") {
		t.Errorf("output missing prefix; got %q", out)
	}
	jsonStart := strings.Index(out, "{")
	if jsonStart < 0 {
		t.Fatalf("no JSON found in output: %q", out)
	}
	var decoded Envelope
	if err := json.Unmarshal([]byte(strings.TrimSpace(out[jsonStart:])), &decoded); err != nil {
		t.Fatalf("unmarshal: %v (out=%q)", err, out)
	}
	if decoded.EmitterID != "tee-test" {
		t.Errorf("decoded.EmitterID = %s", decoded.EmitterID)
	}
}

func TestDebugTeeSink_ForwardsInnerError(t *testing.T) {
	var buf bytes.Buffer
	inner := newMockSink()
	inner.SetSendErr(errFake)
	d := newDebugTeeSink(inner, &buf, nil)
	if err := d.Send(context.Background(), &Envelope{}); err == nil {
		t.Error("Send: nil err, want forwarded inner error")
	}
	if buf.Len() == 0 {
		t.Error("debug write skipped on inner-error path")
	}
}

func TestDebugTeeSink_WriteErrorRoutedToErrLog(t *testing.T) {
	// A writer that fails - errLog should see a single error.
	inner := newMockSink()
	var mu sync.Mutex
	var captured []error
	d := newDebugTeeSink(inner, brokenWriter{}, func(err error) {
		mu.Lock()
		captured = append(captured, err)
		mu.Unlock()
	})
	if err := d.Send(context.Background(), &Envelope{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 1 {
		t.Fatalf("captured = %d errors, want 1", len(captured))
	}
	if !strings.Contains(captured[0].Error(), "debug-tee write") {
		t.Errorf("err = %v, want \"debug-tee write\" substring", captured[0])
	}
}

func TestDebugTeeSink_CloseForwardsToInner(t *testing.T) {
	inner := newMockSink()
	d := newDebugTeeSink(inner, &bytes.Buffer{}, nil)
	if err := d.Close(context.Background()); err != nil {
		t.Errorf("Close: %v", err)
	}
}

var errFake = errFakeT("forced error")

type errFakeT string

func (e errFakeT) Error() string { return string(e) }

// brokenWriter always returns an error from Write.
type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("broken") }

func TestClientNew_WiresDebugTeeWhenEnvSet(t *testing.T) {
	t.Setenv(EnvDebug, "1")
	c := New()
	if _, ok := c.sink.(*debugTeeSink); !ok {
		t.Errorf("c.sink type = %T, want *debugTeeSink when %s=1", c.sink, EnvDebug)
	}
}

func TestClientNew_NoTeeWhenEnvFalsy(t *testing.T) {
	for _, falsy := range []string{"0", "false", ""} {
		t.Run("env="+falsy, func(t *testing.T) {
			t.Setenv(EnvDebug, falsy)
			c := New()
			if _, ok := c.sink.(*debugTeeSink); ok {
				t.Errorf("c.sink wrapped in debugTeeSink with %s=%q; should be untouched", EnvDebug, falsy)
			}
		})
	}
}

func TestDebugTeeSink_ConcurrentSendDoesNotInterleave(t *testing.T) {
	// emit() spawns a goroutine per envelope. With many in-flight
	// sends, the debug-tee's Fprintf calls race the shared writer;
	// without writeMu, large envelopes (> PIPE_BUF) can split
	// mid-write and interleave on stderr. Build a payload large
	// enough to exceed POSIX's PIPE_BUF (4KB) so a non-atomic write
	// would visibly tear.
	var buf concurrentBuffer
	d := newDebugTeeSink(newMockSink(), &buf, nil)

	bigID := strings.Repeat("a", 8000)
	env := &Envelope{SchemaVersion: EnvelopeSchemaVersion1, EmitterID: bigID}

	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			_ = d.Send(context.Background(), env)
		}()
	}
	wg.Wait()

	// Each Send writes one "gaffer-telemetry: ...\n" line. A torn
	// write would land non-prefix bytes mid-line.
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != n {
		t.Fatalf("got %d lines, want %d", len(lines), n)
	}
	for i, line := range lines {
		if !strings.HasPrefix(line, "gaffer-telemetry: ") {
			cut := min(len(line), 64)
			t.Fatalf("line %d torn: %q", i, line[:cut])
		}
	}
}

// concurrentBuffer is a thread-safe sink for the race test above.
// Production code's writeMu is what's actually under test; the
// buffer needs its own lock so the test scaffold isn't the failing
// race.
type concurrentBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *concurrentBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.Write(p)
}

func (c *concurrentBuffer) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

func TestClientNew_TeeWrapsInjectedSinkBehaviourally(t *testing.T) {
	// Replaces the previous pointer-identity check: send an envelope
	// through the client and verify it lands in the injected mock
	// even though the sink-of-record is the debug-tee wrapper.
	t.Setenv(EnvDebug, "1")
	mock := newMockSink()
	c := New(WithSink(mock))
	env := &Envelope{SchemaVersion: EnvelopeSchemaVersion1, EmitterID: "tee-behaviour"}
	c.emit(env)
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if mock.Len() != 1 {
		t.Errorf("mock.Len = %d, want 1 (tee must forward to injected sink)", mock.Len())
	}
}
