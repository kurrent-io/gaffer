package lsp

import (
	"sync"
	"time"
)

// debouncer collapses a stream of per-key events into one fire
// per quiet period: each schedule call cancels any pending fire
// for that key and re-arms a fresh timer. The latest schedule
// wins.
//
// Generic over the work to be performed - each schedule passes
// its own callback. Callers stay responsible for what runs (parse
// + publish, in this package's only consumer); the debouncer
// only owns the timing.
//
// Concurrency: the timer's callback acquires the same mutex the
// scheduler uses. To prevent a stale callback (one whose timer
// fired between Stop and the lock acquisition) from running the
// successor's work or corrupting the map, each callback verifies
// its identity - "is the current entry for this key still me?" -
// and bails if it isn't.
type debouncer struct {
	window time.Duration

	mu     sync.Mutex
	timers map[string]*time.Timer
}

// newDebouncer returns a debouncer that fires `window` after the
// most recent schedule for a key.
func newDebouncer(window time.Duration) *debouncer {
	return &debouncer{
		window: window,
		timers: make(map[string]*time.Timer),
	}
}

// schedule (re-)arms the timer for `key`. After the window
// elapses with no further schedule calls, fn runs in its own
// goroutine.
//
// fn MUST be cancellation-aware: drain() stops the timer but
// can't reach into a callback that already fired - it relies on
// fn observing whatever cancellation signal the caller arranged
// (typically a context the caller captured). A non-cancellable
// fn would outlive shutdown.
func (d *debouncer) schedule(key string, fn func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if t, ok := d.timers[key]; ok {
		t.Stop()
	}
	var timer *time.Timer
	timer = time.AfterFunc(d.window, func() {
		d.mu.Lock()
		if d.timers[key] != timer {
			// Replaced or cancelled while we were queued.
			d.mu.Unlock()
			return
		}
		delete(d.timers, key)
		d.mu.Unlock()
		fn()
	})
	d.timers[key] = timer
}

// cancel stops the pending timer for `key` if any. A callback
// that was already queued will fail its identity check on wake.
func (d *debouncer) cancel(key string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if t, ok := d.timers[key]; ok {
		t.Stop()
		delete(d.timers, key)
	}
}

// drain stops every pending timer. Used at shutdown so callbacks
// that wake later find an empty map and bail.
func (d *debouncer) drain() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for key, t := range d.timers {
		t.Stop()
		delete(d.timers, key)
	}
}
