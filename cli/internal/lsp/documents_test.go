package lsp

import (
	"reflect"
	"sort"
	"sync"
	"testing"
)

func TestDocumentStore_OpenAndGet(t *testing.T) {
	s := NewDocumentStore()
	got := s.Open("file:///a.toml", "hello")
	if got.URI != "file:///a.toml" || got.Content != "hello" || got.Source != SourceMemory {
		t.Fatalf("unexpected open result: %+v", got)
	}
	if got.Version < 1 {
		t.Errorf("expected positive version on open, got %d", got.Version)
	}

	state, ok := s.Get("file:///a.toml")
	if !ok || !reflect.DeepEqual(state, got) {
		t.Fatalf("Get mismatch: %+v ok=%v", state, ok)
	}
}

func TestDocumentStore_VersionMonotonicAcrossClose(t *testing.T) {
	// Pin the load-bearing invariant: Close + reopen produces a
	// strictly higher version than any prior state for the URI.
	// Without this, a stale parse from before the close (with
	// version V) could land after the reopen (whose version
	// resets to 1) and overwrite fresh diagnostics with stale ones.
	s := NewDocumentStore()
	v1 := s.Open("file:///a.toml", "v1").Version
	c, _ := s.Change("file:///a.toml", "v1.1")
	v1Prime := c.Version
	s.Close("file:///a.toml")
	v2 := s.Open("file:///a.toml", "v2").Version
	if v2 <= v1Prime {
		t.Errorf("expected reopen version > prior max %d, got %d", v1Prime, v2)
	}
	if v1 >= v1Prime {
		t.Errorf("change must increment past open: open=%d change=%d", v1, v1Prime)
	}
}

func TestDocumentStore_VersionMonotonicAcrossURIs(t *testing.T) {
	// Versions are issued from a single global counter so a
	// mutation on URI A produces a strictly higher version than
	// the previous mutation on URI B. Pin so a future per-URI
	// counter refactor doesn't silently break the staleness
	// check.
	s := NewDocumentStore()
	a := s.Open("file:///a.toml", "x").Version
	b := s.Open("file:///b.toml", "y").Version
	if b <= a {
		t.Errorf("b should follow a: a=%d b=%d", a, b)
	}
}

func TestDocumentStore_ChangeIncrementsVersion(t *testing.T) {
	s := NewDocumentStore()
	pre := s.Open("file:///a.toml", "first").Version
	got, err := s.Change("file:///a.toml", "second")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "second" {
		t.Errorf("content: got %q want %q", got.Content, "second")
	}
	if got.Version <= pre {
		t.Errorf("Change should increment past Open: pre=%d post=%d", pre, got.Version)
	}
}

func TestDocumentStore_ChangeWithoutOpenIsAnError(t *testing.T) {
	// LSP spec: didChange before didOpen is a client bug. Server
	// returns an error rather than silently auto-promoting; caller
	// logs and drops.
	s := NewDocumentStore()
	if _, err := s.Change("file:///a.toml", "x"); err == nil {
		t.Fatal("expected error on Change of non-open URI")
	}
}

func TestDocumentStore_ChangeAfterAddFromDiskIsRejected(t *testing.T) {
	// Disk-sourced state isn't a client buffer. didChange against
	// it should fail loudly so the caller doesn't accidentally
	// promote disk content to memory via a stray client message.
	s := NewDocumentStore()
	s.AddFromDisk("file:///a.toml", "from disk")
	if _, err := s.Change("file:///a.toml", "x"); err == nil {
		t.Fatal("expected error on Change of disk-sourced URI")
	}
}

func TestDocumentStore_Close(t *testing.T) {
	s := NewDocumentStore()
	s.Open("file:///a.toml", "x")
	if !s.Close("file:///a.toml") {
		t.Error("expected Close to return true for open URI")
	}
	if _, ok := s.Get("file:///a.toml"); ok {
		t.Error("Get should return false after Close")
	}
}

func TestDocumentStore_CloseAbsentIsFalse(t *testing.T) {
	s := NewDocumentStore()
	if s.Close("file:///never.toml") {
		t.Error("expected Close to return false for absent URI")
	}
}

func TestDocumentStore_AddFromDiskOnEmpty(t *testing.T) {
	s := NewDocumentStore()
	state, ok := s.AddFromDisk("file:///a.toml", "from disk")
	if !ok {
		t.Fatal("expected AddFromDisk to succeed on empty slot")
	}
	if state.Source != SourceDisk {
		t.Errorf("expected SourceDisk, got %v", state.Source)
	}
	if state.Content != "from disk" {
		t.Errorf("content: got %q want from disk", state.Content)
	}
}

func TestDocumentStore_AddFromDiskRefreshesDiskState(t *testing.T) {
	// File watcher onChange path: walker / watcher reads new disk
	// content and calls AddFromDisk. Existing disk-sourced state
	// is updated; version bumps so a stale parse can be dropped.
	s := NewDocumentStore()
	pre, _ := s.AddFromDisk("file:///a.toml", "v1")
	got, ok := s.AddFromDisk("file:///a.toml", "v2")
	if !ok {
		t.Fatal("expected refresh to succeed")
	}
	if got.Content != "v2" {
		t.Errorf("content: got %q want v2", got.Content)
	}
	if got.Version <= pre.Version {
		t.Errorf("refresh should bump version: pre=%d post=%d", pre.Version, got.Version)
	}
}

func TestDocumentStore_AddFromDiskSkipsMemoryWins(t *testing.T) {
	// Memory wins: walker comes in with disk content while client
	// already has the buffer open. Skip the disk content so the
	// memory state isn't clobbered with stale on-disk content.
	// On skip, return the existing memory state so a caller that
	// ignores `ok` still has the URI / content it asked about
	// (avoids zero-value footguns).
	s := NewDocumentStore()
	mem := s.Open("file:///a.toml", "in memory")
	got, ok := s.AddFromDisk("file:///a.toml", "from disk")
	if ok {
		t.Fatal("expected AddFromDisk to skip when URI is memory-sourced")
	}
	if got != mem {
		t.Errorf("expected existing memory state on skip, got %+v want %+v", got, mem)
	}
	stored, _ := s.Get("file:///a.toml")
	if stored != mem {
		t.Errorf("memory state changed under us: %+v vs %+v", stored, mem)
	}
}

func TestDocumentStore_OpenURIsReturnsMemorySourcedOnly(t *testing.T) {
	s := NewDocumentStore()
	s.Open("file:///mem-a.toml", "x")
	s.Open("file:///mem-b.toml", "y")
	s.AddFromDisk("file:///disk.toml", "z")

	got := s.OpenURIs()
	want := []string{"file:///mem-a.toml", "file:///mem-b.toml"}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("OpenURIs: got %v want %v", got, want)
	}
}

func TestDocumentStore_GetReturnsByValue(t *testing.T) {
	// Pin the contract: caller can mutate the returned DocState
	// without affecting the store. Defends against accidental
	// shared-mutable-state bugs in higher layers.
	s := NewDocumentStore()
	s.Open("file:///a.toml", "original")
	got, _ := s.Get("file:///a.toml")
	got.Content = "mutated"
	stored, _ := s.Get("file:///a.toml")
	if stored.Content != "original" {
		t.Errorf("Get returned a reference, not a copy: %q", stored.Content)
	}
}

func TestDocumentStore_ConcurrentMutationsRace(t *testing.T) {
	// Race-detector duty: goroutines hammer the store with mixed
	// reads + mutations. The mutex / RWMutex must prevent any
	// data races on docs[]. Open the URI synchronously first so
	// Change is unconditionally legal in the goroutines (avoids
	// scheduling-dependent test flakes).
	s := NewDocumentStore()
	s.Open("file:///shared.toml", "initial")

	const N = 200
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < N; i++ {
			_, _ = s.Change("file:///shared.toml", "x")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < N; i++ {
			_, _ = s.Get("file:///shared.toml")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < N; i++ {
			_ = s.OpenURIs()
		}
	}()
	wg.Wait()

	state, ok := s.Get("file:///shared.toml")
	if !ok {
		t.Fatal("expected URI to remain in store")
	}
	// Initial Open + N Changes = at least N+1 mutations.
	if state.Version < N+1 {
		t.Errorf("expected version >= %d after %d changes, got %d", N+1, N, state.Version)
	}
}
