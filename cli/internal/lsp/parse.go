package lsp

import (
	"context"
	"log"
	"path"

	"github.com/kurrent-io/gaffer/cli/internal/config"
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
	s.publishDiagnostics(uri, emitDiagnostics(desc))
}

// publishDiagnostics sends a textDocument/publishDiagnostics
// notification. Empty Diagnostics intentionally clears squiggles
// for the URI - the client treats it as "no problems here."
func (s *Server) publishDiagnostics(uri string, diags []lspDiagnostic) {
	conn, _ := s.snapshotRunState()
	if conn == nil {
		// Server not connected yet (or already disconnected).
		// Dropping is the right call - the client wouldn't see it.
		return
	}
	if err := conn.Notify(context.Background(), MethodPublishDiagnostics, PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diags,
	}); err != nil {
		// The wire is gone (peer disconnect, write timeout). Not
		// recoverable from here, but worth a log line so a failing
		// client connection is observable.
		log.Printf("lsp: publishDiagnostics %q: %v", uri, err)
	}
}

// isGafferConfig is the parse-trigger gate. V1: any URI whose
// basename is exactly "gaffer.toml". Adding a second format is a
// one-line update here + each editor's activation manifest.
//
// The basename check defends against false positives like
// notgaffer.toml or mygaffer.toml that a naive HasSuffix would
// match.
func isGafferConfig(uri string) bool {
	return path.Base(uriToPath(uri)) == "gaffer.toml"
}
