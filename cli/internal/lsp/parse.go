package lsp

import (
	"context"
	"errors"
	"log"
	"path"
	"strings"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/project"
)

// parseAndPublish parses the current state for URI and, if the
// result is still fresh, caches it and publishes the resulting
// diagnostics. Designed to be called from a goroutine spawned by
// didOpen / didChange handlers - the caller must not hold any
// store locks.
//
// File-extension gate: V1 only parses gaffer.toml. Other URIs the
// client opens (.js entries the user is editing in the same
// workspace, etc.) flow through the document store but don't
// trigger parses; the server isn't a generic TOML LSP.
//
// Cancellation: ctx applies to the parse step. The store mutations
// (look up state, apply result) are short, locked, and not
// cancellable - dropping the result via the staleness check is
// the same outcome as cancelling, with less plumbing.
func (s *Server) parseAndPublish(ctx context.Context, uri string) {
	if !isGafferConfig(uri) {
		return
	}
	state, ok := s.docs.Get(uri)
	if !ok {
		return
	}
	desc, err := config.DescribeBytes(ctx, uriToPath(uri), []byte(state.Content))
	if err != nil {
		// Context cancelled = clean shutdown / debounce supersede;
		// not worth a log line. Anything else (path-resolution, an
		// unexpected parser failure) is real and worth logging so
		// it's diagnosable.
		if ctx.Err() == nil {
			log.Printf("lsp: parse %q: %v", uri, err)
		}
		return
	}
	applied := s.docs.ApplyParseIfFresh(parseResult{
		URI:         uri,
		Version:     state.Version,
		Description: desc,
	})
	if !applied {
		return
	}
	// A new parse may have changed which entry scripts are
	// projection entries (or shifted projection metadata). Open
	// .js buffers showing entry-script lenses derived from the
	// old parse must refresh - publishDiagnostics on the toml
	// only refreshes the toml's own lenses, not any .js URI's.
	//
	// Fired BEFORE publishDiagnostics: the refresh is a
	// fire-and-forget goroutine spawn, but publishDiagnostics
	// holds the conn write lock for its entire wire write. Under
	// contention (concurrent server-side conn.Call or a slow
	// client), publishDiagnostics can stall; ordering it after
	// the refresh keeps the .js lens invalidation off that
	// critical path.
	s.requestCodeLensRefresh()
	s.publishDiagnostics(uri, emitDiagnostics(desc))
}

// publishDiagnostics sends a textDocument/publishDiagnostics
// notification. Empty Diagnostics intentionally clears squiggles
// for the URI - the client treats it as "no problems here."
//
// Bounded by runCtx + a short timeout so a client that's stopped
// reading can't wedge the parse pipeline. Matches the pattern
// used elsewhere for server-pushed messages
// (registerCapability, codeLensRefresh).
func (s *Server) publishDiagnostics(uri string, diags []lspDiagnostic) {
	conn, runCtx := s.snapshotRunState()
	if conn == nil || runCtx == nil {
		// Server not connected yet (or already disconnected).
		// Dropping is the right call - the client wouldn't see it.
		return
	}
	callCtx, cancel := context.WithTimeout(runCtx, 5*time.Second)
	defer cancel()
	if err := conn.Notify(callCtx, MethodPublishDiagnostics, PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diags,
	}); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		log.Printf("lsp: publishDiagnostics %q: %v", uri, err)
	}
}

// gafferConfigGlob is the watcher pattern matching every gaffer config
// file under the workspace. Built from project.ConfigFileName so the
// filename's one source of truth stays in the project package.
const gafferConfigGlob = "**/" + project.ConfigFileName

// isGafferConfig is the parse-trigger gate. V1: any URI whose
// scheme is `file://` and whose basename matches gafferConfigName.
//
// The basename check defends against false positives like
// notgaffer.toml or mygaffer.toml that a naive HasSuffix would
// match. The scheme check defends against non-local URIs
// (vscode-vfs://, untitled:, etc.) that uriToPath would pass
// through unchanged - without it, applyWatchedFileEvents would
// happily route a `vscode-vfs:///gaffer.toml` event into
// seedFromDisk's os.ReadFile.
func isGafferConfig(uri string) bool {
	if !strings.HasPrefix(uri, "file://") {
		return false
	}
	return path.Base(uriToPath(uri)) == project.ConfigFileName
}

// requestCodeLensRefresh asks the client to re-issue every
// outstanding textDocument/codeLens request. Used when something
// changed that could affect lenses on URIs other than the one
// being parsed - specifically, an entry-script .js file's lens
// depends on every cached gaffer.toml's projections, so a parse
// of any toml needs to refresh those .js URIs.
//
// Fire-and-forget on a separate goroutine: a synchronous Call
// from inside parseAndPublish would hold up the parse pipeline
// (and could deadlock against handler dispatch ordering on the
// same conn). We track the goroutine via s.wg so shutdown still
// waits for it to drain.
//
// Best-effort: clients that don't advertise refresh support will
// reject the request and we log + continue. The TOML side still
// gets fresh lenses via publishDiagnostics-triggered refresh, so
// the user-visible cost of an unsupported client is just stale
// .js lenses until the user retriggers the request themselves.
func (s *Server) requestCodeLensRefresh() {
	s.mu.Lock()
	supported := s.codeLensRefreshSupported
	s.mu.Unlock()
	if !supported {
		return
	}
	s.spawn(func() {
		conn, runCtx := s.snapshotRunState()
		if conn == nil || runCtx == nil {
			return
		}
		// 5s is generous; healthy clients ack in single-digit ms.
		// The bound matters because a misbehaving client that
		// never responds would otherwise leave the goroutine
		// blocked until shutdown.
		callCtx, cancel := context.WithTimeout(runCtx, 5*time.Second)
		defer cancel()
		if err := conn.Call(callCtx, MethodCodeLensRefresh, nil, nil); err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Printf("lsp: %s: %v", MethodCodeLensRefresh, err)
		}
	})
}
