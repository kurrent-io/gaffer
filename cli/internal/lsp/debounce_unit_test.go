package lsp

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Direct unit tests on the debouncer type. The integration tests
// in debounce_test.go drive the same code paths through the
// server, but those are slow (100s of ms per test) and harder to
// reason about than these.

func TestDebouncer_FiresAfterWindow(t *testing.T) {
	d := newDebouncer(20 * time.Millisecond)
	var fired atomic.Int32
	d.schedule("k", func() { fired.Add(1) })
	if got := waitUntil(func() int32 { return fired.Load() }, 1, 200*time.Millisecond); got != 1 {
		t.Errorf("expected 1 fire after window, got %d", got)
	}
}

func TestDebouncer_RescheduleResetsWindow(t *testing.T) {
	// Two schedules within one window: only the second's callback
	// fires. The first is replaced and bails its identity check.
	d := newDebouncer(50 * time.Millisecond)
	var firedFirst, firedSecond atomic.Int32
	d.schedule("k", func() { firedFirst.Add(1) })
	time.Sleep(15 * time.Millisecond) // well within the window
	d.schedule("k", func() { firedSecond.Add(1) })
	time.Sleep(150 * time.Millisecond)
	if firedFirst.Load() != 0 {
		t.Errorf("first callback should not have fired: %d", firedFirst.Load())
	}
	if firedSecond.Load() != 1 {
		t.Errorf("second callback should have fired exactly once: %d", firedSecond.Load())
	}
}

func TestDebouncer_CancelStopsTimer(t *testing.T) {
	d := newDebouncer(50 * time.Millisecond)
	var fired atomic.Int32
	d.schedule("k", func() { fired.Add(1) })
	d.cancel("k")
	time.Sleep(150 * time.Millisecond)
	if fired.Load() != 0 {
		t.Errorf("cancelled callback should not fire: %d", fired.Load())
	}
}

func TestDebouncer_DrainStopsAllPending(t *testing.T) {
	d := newDebouncer(50 * time.Millisecond)
	var total atomic.Int32
	for _, k := range []string{"a", "b", "c"} {
		k := k
		d.schedule(k, func() { _ = k; total.Add(1) })
	}
	d.drain()
	time.Sleep(150 * time.Millisecond)
	if total.Load() != 0 {
		t.Errorf("drain should have prevented all callbacks: %d", total.Load())
	}
}

func TestDebouncer_PerKeyIndependence(t *testing.T) {
	// Bursting on key A doesn't delay B's fire.
	d := newDebouncer(50 * time.Millisecond)
	var firedA, firedB atomic.Int32
	for i := 0; i < 5; i++ {
		d.schedule("a", func() { firedA.Add(1) })
		time.Sleep(10 * time.Millisecond)
	}
	d.schedule("b", func() { firedB.Add(1) })
	time.Sleep(150 * time.Millisecond)
	if firedA.Load() != 1 {
		t.Errorf("expected exactly 1 fire on A, got %d", firedA.Load())
	}
	if firedB.Load() != 1 {
		t.Errorf("expected exactly 1 fire on B, got %d", firedB.Load())
	}
}

func TestDebouncer_LateCallbackBailsOnIdentityCheck(t *testing.T) {
	// Stress: many concurrent schedules on the same key. After the
	// dust settles, exactly one callback runs - the last one. This
	// pins the identity-check that protects against AfterFunc's
	// Stop() losing the race against an already-queued callback.
	d := newDebouncer(30 * time.Millisecond)
	var fired atomic.Int32
	const goroutines = 50
	const itersPerGoroutine = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < itersPerGoroutine; i++ {
				d.schedule("k", func() { fired.Add(1) })
			}
		}()
	}
	wg.Wait()
	time.Sleep(200 * time.Millisecond)
	if got := fired.Load(); got != 1 {
		t.Errorf("expected exactly 1 fire after burst, got %d", got)
	}
}

// waitUntil polls fn() until it returns want or timeout elapses.
// Returns the last observed value.
func waitUntil[T comparable](fn func() T, want T, timeout time.Duration) T {
	deadline := time.Now().Add(timeout)
	var last T
	for time.Now().Before(deadline) {
		last = fn()
		if last == want {
			return last
		}
		time.Sleep(5 * time.Millisecond)
	}
	return last
}
