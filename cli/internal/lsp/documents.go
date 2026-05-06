package lsp

import (
	"fmt"
	"sort"
	"sync"
)

// Source tracks where a document's current content came from. The
// LSP server prefers memory-sourced state over disk state - if a
// client has the file open, the in-memory buffer is authoritative
// and the workspace walker must not overwrite it. See the LSP
// plan's "Document state and concurrency" section.
type Source int

const (
	// SourceDisk: content was read from the filesystem (e.g. by the
	// workspace walker on initialize, or by a file-watcher event).
	SourceDisk Source = iota
	// SourceMemory: content came from a client buffer via didOpen
	// or didChange. Authoritative over disk.
	SourceMemory
)

// DocState is a snapshot of a document's content + provenance.
// Returned by-value from DocumentStore methods; callers can mutate
// their copy without affecting the store.
type DocState struct {
	URI     string
	Content string
	// Version is per-URI monotonic. Each mutation (Open, Change,
	// AddFromDisk update) increments it. Parse goroutines stamp
	// the version they observed at parse-start so out-of-order
	// completions can be dropped on apply.
	Version int
	Source  Source
}

// DocumentStore is the thread-safe source of truth for what
// content the LSP server believes is in each document. Memory
// state (from didOpen/didChange) wins over disk state (from the
// walker / file watcher) for the same URI.
//
// The store is purely a data structure - it has no opinions on
// parsing, debouncing, or publishing. Those live in higher
// layers; the store just gives them a coherent view of who said
// what when.
//
// Versioning: a single monotonic counter ticks for every mutation
// across all URIs. Each DocState's Version is the counter value
// at the time of its last mutation. The counter does NOT reset on
// Close - that's load-bearing for the parse-staleness check (a
// stale parse from before a Close would otherwise have a higher
// version than the post-reopen state and silently overwrite it).
type DocumentStore struct {
	mu      sync.RWMutex
	docs    map[string]DocState
	nextVer int
}

// NewDocumentStore returns an empty store.
func NewDocumentStore() *DocumentStore {
	return &DocumentStore{docs: map[string]DocState{}}
}

// bumpLocked returns the next version. Caller must hold s.mu.
func (s *DocumentStore) bumpLocked() int {
	s.nextVer++
	return s.nextVer
}

// Open records a client buffer for URI. Memory-sourced. Version
// pulls from the global counter so it monotonically advances even
// across Close + reopen cycles for the same URI.
func (s *DocumentStore) Open(uri, content string) DocState {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := DocState{
		URI:     uri,
		Content: content,
		Version: s.bumpLocked(),
		Source:  SourceMemory,
	}
	s.docs[uri] = state
	return state
}

// Change updates the content of an already-open buffer. Increments
// version. Returns an error if URI isn't currently open - LSP spec
// makes this a client error and the caller should log + drop.
func (s *DocumentStore) Change(uri, content string) (DocState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, ok := s.docs[uri]
	if !ok {
		return DocState{}, fmt.Errorf("change before didOpen: %s", uri)
	}
	if prev.Source != SourceMemory {
		return DocState{}, fmt.Errorf("change requires memory-sourced URI (was disk): %s", uri)
	}
	state := DocState{
		URI:     uri,
		Content: content,
		Version: s.bumpLocked(),
		Source:  SourceMemory,
	}
	s.docs[uri] = state
	return state, nil
}

// Close removes a URI from the store. Returns true if the URI was
// present. The global version counter does NOT roll back - a
// future reopen gets a strictly-greater version than any prior
// state for the URI.
func (s *DocumentStore) Close(uri string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.docs[uri]
	if ok {
		delete(s.docs, uri)
	}
	return ok
}

// Get returns the current state for URI and ok=true if any state
// is recorded. The returned DocState is a copy.
func (s *DocumentStore) Get(uri string) (DocState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.docs[uri]
	return state, ok
}

// OpenURIs returns the memory-sourced URIs in lexicographic order.
// The walker uses this to skip files the client already has open
// (memory wins).
func (s *DocumentStore) OpenURIs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.docs))
	for uri, state := range s.docs {
		if state.Source == SourceMemory {
			out = append(out, uri)
		}
	}
	sort.Strings(out)
	return out
}

// AddFromDisk seeds (or refreshes) disk-sourced content for URI.
// No-op if the URI is currently memory-sourced - memory wins,
// caller should NOT proceed to parse/publish based on the disk
// content it brought in.
//
// Returns the post-call state and ok=true on success. On a
// memory-wins skip returns the existing memory state and ok=false
// so a caller that misuses the result still has useful info
// (the URI it asked about) instead of zero values.
func (s *DocumentStore) AddFromDisk(uri, content string) (DocState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.docs[uri]; ok && prev.Source == SourceMemory {
		return prev, false
	}
	state := DocState{
		URI:     uri,
		Content: content,
		Version: s.bumpLocked(),
		Source:  SourceDisk,
	}
	s.docs[uri] = state
	return state, true
}
