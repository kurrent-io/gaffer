package lsp

import (
	"fmt"
	"sort"
	"sync"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

// source tracks where a document's current content came from. The
// LSP server prefers memory-sourced state over disk state - if a
// client has the file open, the in-memory buffer is authoritative
// and the workspace walker must not overwrite it. See the LSP
// plan's "Document state and concurrency" section.
type source int

const (
	// sourceDisk: content was read from the filesystem (e.g. by the
	// workspace walker on initialize, or by a file-watcher event).
	sourceDisk source = iota
	// sourceMemory: content came from a client buffer via didOpen
	// or didChange. Authoritative over disk.
	sourceMemory
)

// docState is a snapshot of a document's content + provenance.
// Returned by-value from documentStore methods; callers can mutate
// their copy without affecting the store.
type docState struct {
	URI     string
	Content string
	// Version is per-URI monotonic. Each mutation (Open, Change,
	// AddFromDisk update) increments it. Parse goroutines stamp
	// the version they observed at parse-start so out-of-order
	// completions can be dropped on apply.
	Version int
	Source  source
}

// documentStore is the thread-safe source of truth for what
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
// across all URIs. Each docState's Version is the counter value
// at the time of its last mutation. The counter does NOT reset on
// Close - that's load-bearing for the parse-staleness check (a
// stale parse from before a Close would otherwise have a higher
// version than the post-reopen state and silently overwrite it).
type documentStore struct {
	mu      sync.RWMutex
	docs    map[string]docState
	parses  map[string]parseResult
	nextVer int
}

// parseResult is the cached output of a parse pass for a URI. The
// codeLens handler reads from this; the parse pipeline writes to
// it via ApplyParseIfFresh. Version is the docState.Version that
// was current when parsing began - used to drop stale results.
type parseResult struct {
	URI         string
	Version     int
	Description config.Description
}

// newDocumentStore returns an empty store.
func newDocumentStore() *documentStore {
	return &documentStore{
		docs:   map[string]docState{},
		parses: map[string]parseResult{},
	}
}

// bumpLocked returns the next version. Caller must hold s.mu.
func (s *documentStore) bumpLocked() int {
	s.nextVer++
	return s.nextVer
}

// Open records a client buffer for URI. Memory-sourced. Version
// pulls from the global counter so it monotonically advances even
// across Close + reopen cycles for the same URI.
func (s *documentStore) Open(uri, content string) docState {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := docState{
		URI:     uri,
		Content: content,
		Version: s.bumpLocked(),
		Source:  sourceMemory,
	}
	s.docs[uri] = state
	return state
}

// Change updates the content of an already-open buffer. Increments
// version. Returns an error if URI isn't currently open - LSP spec
// makes this a client error and the caller should log + drop.
func (s *documentStore) Change(uri, content string) (docState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, ok := s.docs[uri]
	if !ok {
		return docState{}, fmt.Errorf("change before didOpen: %s", uri)
	}
	if prev.Source != sourceMemory {
		return docState{}, fmt.Errorf("change requires memory-sourced URI (was disk): %s", uri)
	}
	state := docState{
		URI:     uri,
		Content: content,
		Version: s.bumpLocked(),
		Source:  sourceMemory,
	}
	s.docs[uri] = state
	return state, nil
}

// Close removes a URI from the store. Returns true if the URI was
// present. The global version counter does NOT roll back - a
// future reopen gets a strictly-greater version than any prior
// state for the URI.
//
// Also drops any cached parse result for the URI so a future
// reopen starts with a clean slate (stale lenses won't survive
// the close).
func (s *documentStore) Close(uri string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.docs[uri]
	if ok {
		delete(s.docs, uri)
	}
	delete(s.parses, uri)
	return ok
}

// Get returns the current state for URI and ok=true if any state
// is recorded. The returned docState is a copy.
func (s *documentStore) Get(uri string) (docState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.docs[uri]
	return state, ok
}

// OpenURIs returns the memory-sourced URIs in lexicographic order.
// The walker uses this to skip files the client already has open
// (memory wins).
func (s *documentStore) OpenURIs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.docs))
	for uri, state := range s.docs {
		if state.Source == sourceMemory {
			out = append(out, uri)
		}
	}
	sort.Strings(out)
	return out
}

// ApplyParseIfFresh stores a parse result iff it's still fresh -
// i.e. the URI's current state version isn't newer than the
// version observed at parse start. Stale results are dropped.
//
// Returns true when the result was applied; callers use this to
// gate publishDiagnostics emission so a stale parse can't push
// out-of-date squiggles. Returns false in two cases:
//   - URI was closed mid-parse (no state).
//   - State has advanced past the parse's stamped version.
func (s *documentStore) ApplyParseIfFresh(result parseResult) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.docs[result.URI]
	if !ok {
		return false
	}
	if state.Version > result.Version {
		return false
	}
	s.parses[result.URI] = result
	return true
}

// GetParse returns the cached parse for URI, ok=true if present.
// Used by the codeLens request handler to render lenses without
// re-parsing.
func (s *documentStore) GetParse(uri string) (parseResult, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.parses[uri]
	return r, ok
}

// AllParses returns every cached parse, in arbitrary order. Used
// by entry-script codeLens lookup and workspace/symbol to walk
// every parsed projection across the workspace.
//
// Concurrency: the returned slice is a fresh copy so callers can
// mutate it. The parseResult values share their Description with
// the store, but ApplyParseIfFresh always replaces an entry
// rather than mutating in place, so a reader holding a copy
// observes a consistent snapshot for that URI even if a fresh
// parse lands during iteration.
func (s *documentStore) AllParses() []parseResult {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]parseResult, 0, len(s.parses))
	for _, r := range s.parses {
		out = append(out, r)
	}
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
func (s *documentStore) AddFromDisk(uri, content string) (docState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.docs[uri]; ok && prev.Source == sourceMemory {
		return prev, false
	}
	state := docState{
		URI:     uri,
		Content: content,
		Version: s.bumpLocked(),
		Source:  sourceDisk,
	}
	s.docs[uri] = state
	return state, true
}
