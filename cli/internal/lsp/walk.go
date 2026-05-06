package lsp

import (
	"context"
	"errors"
	"log"
	"os"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/sourcegraph/jsonrpc2"
)

// handleInitialized registers the workspace/didChangeWatchedFiles
// capability and then walks workspace roots for gaffer.toml files.
// Runs in a goroutine because it does I/O.
//
// Register-then-walk so disk events landing during the walk reach
// the server (the editor isn't watching until we ask). A Created
// event for a file the walk also discovers can race the walk's
// own seedFromDisk - AddFromDisk is locked and the parse staleness
// check drops out-of-order results, so the cost is at most a
// duplicate parse, never stale state.
func (s *Server) handleInitialized(ctx context.Context) {
	s.registerFileWatcher(ctx)
	s.walkWorkspaces(ctx)
}

// walkWorkspaces walks each captured root and seeds disk content
// for each discovered gaffer.toml. Memory-sourced URIs (open client
// buffers) are not overwritten. Per-root errors are logged and the
// walk continues - one stale workspace path shouldn't poison the
// rest.
func (s *Server) walkWorkspaces(ctx context.Context) {
	s.mu.Lock()
	roots := append([]string(nil), s.roots...)
	s.mu.Unlock()
	for _, root := range roots {
		if ctx.Err() != nil {
			return
		}
		paths, err := config.WalkConfigs(ctx, root)
		if err != nil {
			log.Printf("lsp: walk %q: %v", root, err)
			continue
		}
		for _, path := range paths {
			if ctx.Err() != nil {
				return
			}
			s.seedFromDisk(ctx, path)
		}
	}
}

// seedFromDisk reads the file at path and feeds it through
// AddFromDisk. If the URI is already memory-sourced (open buffer),
// AddFromDisk skips the write and we don't reparse - the buffer
// remains authoritative.
//
// ENOENT is silent: a file present at walk time can vanish before
// the read, and a watcher Created event can race a Deleted. The
// watcher will catch up either way.
func (s *Server) seedFromDisk(ctx context.Context, path string) {
	uri := pathToURI(path)
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("lsp: read %q: %v", path, err)
		}
		return
	}
	if _, ok := s.docs.AddFromDisk(uri, string(data)); !ok {
		return
	}
	s.parseAndPublish(ctx, uri)
}

// registerFileWatcher sends a client/registerCapability request
// asking the editor to watch every `gaffer.toml` in the workspace.
// File watching is dynamic-only per the LSP spec.
//
// Best-effort: if the client doesn't support dynamic registration
// the request fails and the editor just won't push live disk events.
// The walk-based seed already reflects current state.
func (s *Server) registerFileWatcher(ctx context.Context) {
	conn, _ := s.snapshotRunState()
	if conn == nil {
		return
	}
	params := RegistrationParams{
		Registrations: []Registration{{
			ID:     "gaffer-config-watcher",
			Method: MethodDidChangeWatchedFiles,
			RegisterOptions: DidChangeWatchedFilesRegistrationOptions{
				Watchers: []FileSystemWatcher{
					{GlobPattern: gafferConfigGlob},
				},
			},
		}},
	}
	// Bound the call - a misbehaving client that ACKed initialize
	// but never responds to capability registration would otherwise
	// wedge this goroutine until shutdown. 30s is generous; a
	// healthy client responds in single-digit ms.
	callCtx, cancelCall := context.WithTimeout(ctx, 30*time.Second)
	defer cancelCall()
	if err := conn.Call(callCtx, MethodRegisterCapability, params, nil); err != nil {
		// Cancellation = clean shutdown raced the registration -
		// don't pollute logs.
		if errors.Is(err, context.Canceled) {
			return
		}
		log.Printf("lsp: registerCapability: %v", err)
	}
}

// handleDidChangeWatchedFiles processes file events the editor
// pushed in. Created/Changed re-read disk; Deleted drops state
// and clears diagnostics. Non-gaffer URIs are filtered.
//
// Events are processed sequentially in a single spawned goroutine
// so that a [Changed, Deleted] burst for the same URI applies in
// the order the editor reported - otherwise an async Changed could
// re-seed the URI after a synchronous Deleted closed it.
func (s *Server) handleDidChangeWatchedFiles(_ context.Context, req *jsonrpc2.Request) (interface{}, error) {
	if req.Params == nil {
		return nil, nil
	}
	params, jerr := decodeParams[DidChangeWatchedFilesParams](req, "didChangeWatchedFiles")
	if jerr != nil {
		return nil, jerr
	}
	events := params.Changes
	s.spawnWithCtx(func(runCtx context.Context) {
		s.applyWatchedFileEvents(runCtx, events)
	})
	return nil, nil
}

// applyWatchedFileEvents replays a batch of file events in order on
// a single goroutine. Order matters: Created/Changed reads disk and
// inserts; Deleted closes the URI - mixing these on separate
// goroutines races on a [Changed, Deleted] burst.
func (s *Server) applyWatchedFileEvents(ctx context.Context, events []FileEvent) {
	for _, ev := range events {
		if ctx.Err() != nil {
			return
		}
		if !isGafferConfig(ev.URI) {
			continue
		}
		switch ev.Type {
		case FileChangeCreated, FileChangeChanged:
			s.seedFromDisk(ctx, uriToPath(ev.URI))
		case FileChangeDeleted:
			_, hadParse := s.docs.GetParse(ev.URI)
			s.docs.Close(ev.URI)
			s.publishDiagnostics(ev.URI, []lspDiagnostic{})
			if hadParse {
				// Cached parse for this toml is gone - any .js
				// URI whose lenses pointed at one of its
				// projections is now stale.
				s.requestCodeLensRefresh()
			}
		}
	}
}
