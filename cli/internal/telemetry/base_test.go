package telemetry

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCaptureStack_NilNoPanic(t *testing.T) {
	val, frames := captureStack(nil)
	if val != nil || frames != nil {
		t.Errorf("captureStack(nil) = (%v, %d frames)", val, len(frames))
	}
}

func TestCaptureStack_CapturesValueAndFrames(t *testing.T) {
	var (
		val    any
		frames []runtime.Frame
	)
	func() {
		defer func() {
			val, frames = captureStack(recover())
		}()
		panic("boom")
	}()
	if val != "boom" {
		t.Errorf("val = %v, want \"boom\"", val)
	}
	if len(frames) == 0 {
		t.Fatal("frames empty")
	}
	var found bool
	for _, f := range frames {
		if strings.Contains(f.Function, "TestCaptureStack_CapturesValueAndFrames") {
			found = true
			break
		}
	}
	if !found {
		var funcs []string
		for _, f := range frames {
			funcs = append(funcs, f.Function)
		}
		t.Errorf("no frame mentions the test function; got: %v", funcs)
	}
}

func TestCaptureStack_PanicValueIsError(t *testing.T) {
	target := errors.New("boom-error")
	var val any
	func() {
		defer func() {
			val, _ = captureStack(recover())
		}()
		panic(target)
	}()
	gotErr, ok := val.(error)
	if !ok {
		t.Fatalf("val type = %T, want error", val)
	}
	if !errors.Is(gotErr, target) {
		t.Errorf("val = %v, want %v", gotErr, target)
	}
}

func TestCaptureStack_FrameCountCapped(t *testing.T) {
	var frames []runtime.Frame
	func() {
		defer func() {
			_, frames = captureStack(recover())
		}()
		deepPanic(200)
	}()
	if len(frames) > maxStackFrames {
		t.Errorf("len(frames) = %d, want <= %d", len(frames), maxStackFrames)
	}
	if len(frames) == 0 {
		t.Error("frames was empty")
	}
}

func deepPanic(n int) {
	if n == 0 {
		panic("deep")
	}
	deepPanic(n - 1)
}

func TestClient_DefaultErrorLogIsSilent(t *testing.T) {
	c := New()
	c.errLog(errors.New("first"))
	c.errLog(errors.New("second"))
}

func TestClient_EmitSendsEnvelope(t *testing.T) {
	mock := newMockSink()
	c := New(WithSink(mock))
	env := &Envelope{
		SchemaVersion: SchemaVersion,
		EmitterID:     "abc",
		RunID:         "def",
		Context: Context{
			Emitter: EmitterCLI, LibVersion: "0.0.0", OS: OSLinux, Arch: ArchX64,
			RuntimeEnvironment: RuntimeEnvironmentLocal,
		},
	}
	c.emit(env)
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	envs := mock.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envs))
	}
	if envs[0].EmitterID != "abc" {
		t.Errorf("EmitterID = %s", envs[0].EmitterID)
	}
}

func TestClient_EmitMany(t *testing.T) {
	mock := newMockSink()
	c := New(WithSink(mock))
	const n = 50
	for i := 0; i < n; i++ {
		c.emit(&Envelope{SchemaVersion: SchemaVersion, EmitterID: "y"})
	}
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := mock.Len(); got != n {
		t.Errorf("len = %d, want %d", got, n)
	}
}

func TestClient_FlushTimeoutWraps(t *testing.T) {
	mock := newMockSink()
	mock.SetDelay(500 * time.Millisecond)
	c := New(WithSink(mock))
	c.emit(&Envelope{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := c.Flush(ctx)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Flush err = %v, want DeadlineExceeded", err)
	}
}

func TestClient_FlushNilReceiverIsNoop(t *testing.T) {
	var c *Client
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Errorf("Flush on nil = %v, want nil", err)
	}
}

func TestClient_FlushIsIdempotent(t *testing.T) {
	mock := newMockSink()
	c := New(WithSink(mock))
	c.emit(&Envelope{SchemaVersion: SchemaVersion, EmitterID: "z"})
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("Flush 1: %v", err)
	}
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("Flush 2: %v", err)
	}
	if got := mock.Len(); got != 1 {
		t.Errorf("len = %d, want 1", got)
	}
}

func TestClient_EmitAfterFlushIsDropped(t *testing.T) {
	mock := newMockSink()
	c := New(WithSink(mock))
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	c.emit(&Envelope{SchemaVersion: SchemaVersion, EmitterID: "should-drop"})
	time.Sleep(20 * time.Millisecond)
	if got := mock.Len(); got != 0 {
		t.Errorf("len = %d, want 0 (post-flush emits must drop silently)", got)
	}
}

// TestClient_EmitFlushRaceStress hammers emit and Flush together to look
// for the WaitGroup Add-after-Wait window the atomic-flag-only form had
// (concurrent emit reads closed=false, Flush sets closed and Wait returns,
// emit then Add(1) - goroutine leaks past Flush, or panics if Wait was
// still in flight). Uses a barrier so both goroutines start at the same
// instant and runs many iterations to stress the window.
func TestClient_EmitFlushRaceStress(t *testing.T) {
	const iters = 200
	for i := 0; i < iters; i++ {
		mock := newMockSink()
		c := New(WithSink(mock))

		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			c.emit(&Envelope{})
		}()
		go func() {
			defer wg.Done()
			<-start
			_ = c.Flush(timeoutCtx(t, time.Second))
		}()
		close(start)
		wg.Wait()

		// Whichever order won, no panic must have escaped. The envelope
		// either landed (emit beat Flush) or was dropped (Flush won).
		if n := mock.Len(); n != 0 && n != 1 {
			t.Fatalf("iter %d: mock.Len = %d, want 0 or 1", i, n)
		}
	}
}

func TestClient_PerSendTimeoutLogged(t *testing.T) {
	mock := newMockSink()
	mock.SetDelay(200 * time.Millisecond)
	var (
		mu       sync.Mutex
		errCount atomic.Int32
		lastErr  error
	)
	c := New(
		WithSink(mock),
		WithPerSendTimeout(20*time.Millisecond),
		WithErrorLogger(func(err error) {
			errCount.Add(1)
			mu.Lock()
			lastErr = err
			mu.Unlock()
		}),
	)
	c.emit(&Envelope{})
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if errCount.Load() != 1 {
		t.Errorf("errCount = %d, want 1", errCount.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if lastErr == nil || !strings.Contains(lastErr.Error(), "send failed") {
		t.Errorf("err = %v, want \"send failed\" substring", lastErr)
	}
}

func TestClient_EmitDoesNotBlock(t *testing.T) {
	mock := newMockSink()
	mock.SetDelay(time.Hour)
	c := New(WithSink(mock), WithPerSendTimeout(10*time.Millisecond))
	start := time.Now()
	c.emit(&Envelope{})
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("emit blocked %v, expected ~immediate return", elapsed)
	}
	_ = c.Flush(timeoutCtx(t, 500*time.Millisecond))
}

// panickingSink panics on Send to verify Client recovers inside the
// goroutine and reports via errLog rather than killing the process.
type panickingSink struct{}

func (panickingSink) Send(_ context.Context, _ *Envelope) error { panic("sink boom") }
func (panickingSink) Close(_ context.Context) error             { return nil }

func TestClient_RecoversFromPanickingSink(t *testing.T) {
	var lastErr error
	var mu sync.Mutex
	c := New(
		WithSink(panickingSink{}),
		WithErrorLogger(func(err error) {
			mu.Lock()
			lastErr = err
			mu.Unlock()
		}),
	)
	c.emit(&Envelope{})
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if lastErr == nil || !strings.Contains(lastErr.Error(), "panicked") {
		t.Errorf("err = %v, want \"panicked\" substring", lastErr)
	}
}

// closeCountingSink lets us assert Close is called from Flush.
type closeCountingSink struct {
	mock     *internalMockSink
	closeCnt atomic.Int32
	closeErr error
}

func (s *closeCountingSink) Send(ctx context.Context, env *Envelope) error {
	return s.mock.Send(ctx, env)
}

func (s *closeCountingSink) Close(ctx context.Context) error {
	s.closeCnt.Add(1)
	return s.closeErr
}

func TestClient_FlushCallsSinkClose(t *testing.T) {
	cs := &closeCountingSink{mock: newMockSink()}
	c := New(WithSink(cs))
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if cs.closeCnt.Load() != 1 {
		t.Errorf("close count = %d, want 1", cs.closeCnt.Load())
	}
}

func TestClient_FlushCallsCloseEvenOnTimeout(t *testing.T) {
	cs := &closeCountingSink{mock: newMockSink()}
	cs.mock.SetDelay(500 * time.Millisecond)
	c := New(WithSink(cs))
	c.emit(&Envelope{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_ = c.Flush(ctx)
	if cs.closeCnt.Load() != 1 {
		t.Errorf("close count = %d, want 1 (Close runs on timeout path too)", cs.closeCnt.Load())
	}
}

func timeoutCtx(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}
