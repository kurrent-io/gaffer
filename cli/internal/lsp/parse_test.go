package lsp

import (
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"
)

// capturedNotify records a single incoming server-pushed
// notification on the test-side connection.
type capturedNotify struct {
	Method string
	Params json.RawMessage
}

// notifyCapture is a test-only client handler that records every
// incoming server-pushed notification. The lsp server pushes
// publishDiagnostics; tests assert on what arrived.
type notifyCapture struct {
	mu    sync.Mutex
	calls []capturedNotify
}

func (n *notifyCapture) handler(_ context.Context, _ *jsonrpc2.Conn, req *jsonrpc2.Request) (interface{}, error) {
	if !req.Notif {
		return nil, nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	var raw json.RawMessage
	if req.Params != nil {
		raw = *req.Params
	}
	n.calls = append(n.calls, capturedNotify{Method: req.Method, Params: raw})
	return nil, nil
}

func (n *notifyCapture) snapshot() []capturedNotify {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]capturedNotify, len(n.calls))
	copy(out, n.calls)
	return out
}

// newClientConnCapturing is like newClientConn but routes incoming
// server notifications into the given capture instead of dropping
// them. Tests use this when they need to assert on what the server
// pushed.
func newClientConnCapturing(ctx context.Context, stream io.ReadWriteCloser, n *notifyCapture) *jsonrpc2.Conn {
	return jsonrpc2.NewConn(
		ctx,
		jsonrpc2.NewBufferedStream(stream, jsonrpc2.VSCodeObjectCodec{}),
		jsonrpc2.HandlerWithError(n.handler),
	)
}

// findPublishDiagnostics returns the most-recent
// publishDiagnostics notification for `uri`, or nil if none.
func findPublishDiagnostics(calls []capturedNotify, uri string) *PublishDiagnosticsParams {
	var latest *PublishDiagnosticsParams
	for i := range calls {
		if calls[i].Method != MethodPublishDiagnostics {
			continue
		}
		var p PublishDiagnosticsParams
		if err := json.Unmarshal(calls[i].Params, &p); err != nil {
			continue
		}
		if p.URI == uri {
			latest = &p
		}
	}
	return latest
}

// countPublishDiagnostics returns the number of publishDiagnostics
// notifications recorded for `uri`. Useful for asserting that a
// debounce collapsed N events into 1 publish.
func countPublishDiagnostics(calls []capturedNotify, uri string) int {
	count := 0
	for i := range calls {
		if calls[i].Method != MethodPublishDiagnostics {
			continue
		}
		var p PublishDiagnosticsParams
		if err := json.Unmarshal(calls[i].Params, &p); err != nil {
			continue
		}
		if p.URI == uri {
			count++
		}
	}
	return count
}

// tempTOMLPath returns the absolute path + file URI for a
// gaffer.toml under a fresh t.TempDir. Doesn't write the file -
// parseAndPublish reads from the in-memory document store, not
// disk, so the file's existence is irrelevant.
func tempTOMLPath(t *testing.T) (path, uri string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "gaffer.toml")
	uri = pathToURI(path)
	return path, uri
}

func TestServer_DidOpenPushesDiagnostics(t *testing.T) {
	// A toml with an invalid fixture path should produce a
	// publishDiagnostics notification with the rule code attached.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
	defer cancel()

	_, done := startServerWithStore(ctx, srv, ServerOptions{})
	n := &notifyCapture{}
	conn := newClientConnCapturing(ctx, cli, n)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})

	_, uri := tempTOMLPath(t)
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, LanguageID: "toml", Text: `[[projection]]
name = "p"
entry = "p.js"
fixtures.evil = "../escape.json"
`},
	})

	waitFor(t, func() bool {
		return findPublishDiagnostics(n.snapshot(), uri) != nil
	}, waitForTimeout)

	got := findPublishDiagnostics(n.snapshot(), uri)
	if got == nil {
		t.Fatal("expected a publishDiagnostics for the URI")
	}
	if len(got.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %v", got.Diagnostics)
	}
	d := got.Diagnostics[0]
	if d.Code != "fixture.path-escapes-root" {
		t.Errorf("rule code: got %q want fixture.path-escapes-root", d.Code)
	}
	if d.Severity != diagnosticSeverityError {
		t.Errorf("severity: got %d want Error", d.Severity)
	}
	if d.Source != "gaffer" {
		t.Errorf("source: got %q want gaffer", d.Source)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_DidOpenForNonGafferFileSkipsParse(t *testing.T) {
	// Client opens a random .js file the user is editing in the
	// same workspace. Should be stored (so future watchers /
	// extensions could read it) but NOT parsed - we only parse
	// gaffer.toml.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
	defer cancel()

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	n := &notifyCapture{}
	conn := newClientConnCapturing(ctx, cli, n)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})
	uri := "file:///workspace/projection.js"
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, LanguageID: "javascript", Text: "fromAll()"},
	})
	waitFor(t, func() bool {
		_, ok := server.docs.Get(uri)
		return ok
	}, waitForTimeout)
	// Sleep a touch to allow any spurious parse goroutine to
	// publish; if the gate is right, no notification arrives.
	time.Sleep(100 * time.Millisecond)
	if got := findPublishDiagnostics(n.snapshot(), uri); got != nil {
		t.Errorf("expected no publishDiagnostics for non-gaffer URI, got %+v", got)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_DidChangeRepublishesDiagnostics(t *testing.T) {
	// Initial content is invalid; didChange fixes it. Latest
	// publishDiagnostics for the URI should clear the squiggles
	// (zero diagnostics).
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
	defer cancel()

	_, done := startServerWithStore(ctx, srv, ServerOptions{})
	n := &notifyCapture{}
	conn := newClientConnCapturing(ctx, cli, n)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})

	_, uri := tempTOMLPath(t)
	bad := `[[projection]]
name = "p"
entry = "p.js"
fixtures.evil = "../escape.json"
`
	good := `[[projection]]
name = "p"
entry = "p.js"
fixtures.happy = "fixtures/happy.json"
`
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: bad},
	})
	waitFor(t, func() bool {
		got := findPublishDiagnostics(n.snapshot(), uri)
		return got != nil && len(got.Diagnostics) == 1
	}, waitForTimeout)

	_ = conn.Notify(ctx, MethodDidChange, &DidChangeTextDocumentParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uri},
		ContentChanges: []TextDocumentContentChangeEvent{{Text: good}},
	})
	waitFor(t, func() bool {
		got := findPublishDiagnostics(n.snapshot(), uri)
		return got != nil && len(got.Diagnostics) == 0
	}, waitForTimeout)

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_DidCloseClearsDiagnostics(t *testing.T) {
	// Closing a previously-invalid URI must clear the squiggles
	// in the editor's Problems panel - else stale diagnostics
	// linger after the file has been hidden.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
	defer cancel()

	_, done := startServerWithStore(ctx, srv, ServerOptions{})
	n := &notifyCapture{}
	conn := newClientConnCapturing(ctx, cli, n)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})
	_, uri := tempTOMLPath(t)
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: `[[projection]]
name = "p"
entry = "p.js"
fixtures.evil = "../escape.json"
`},
	})
	waitFor(t, func() bool {
		got := findPublishDiagnostics(n.snapshot(), uri)
		return got != nil && len(got.Diagnostics) == 1
	}, waitForTimeout)

	_ = conn.Notify(ctx, MethodDidClose, &DidCloseTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	})
	waitFor(t, func() bool {
		got := findPublishDiagnostics(n.snapshot(), uri)
		return got != nil && len(got.Diagnostics) == 0
	}, waitForTimeout)

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_CodeLensRequestReturnsLenses(t *testing.T) {
	// After didOpen on a valid toml, textDocument/codeLens should
	// return the projection-level Debug lens, the dropdown, and
	// the per-fixture lens.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
	defer cancel()

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})

	_, uri := tempTOMLPath(t)
	content := `[[projection]]
name = "checkout"
entry = "checkout.js"
fixtures.happy = "fixtures/happy.json"

[env.local]
connection = "kurrentdb://localhost:2113?tls=false"
default = true
`
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: content},
	})
	waitFor(t, func() bool {
		_, ok := server.docs.GetParse(uri)
		return ok
	}, waitForTimeout)

	var lenses []CodeLens
	if err := conn.Call(ctx, MethodCodeLens, CodeLensParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}, &lenses); err != nil {
		t.Fatalf("codeLens: %v", err)
	}
	// projection Debug + per-fixture Debug + dropdown = 3
	if len(lenses) != 3 {
		t.Fatalf("expected 3 lenses, got %d: %+v", len(lenses), lenses)
	}
	intents := []string{}
	for _, l := range lenses {
		if l.Data == nil {
			t.Fatalf("lens missing data.intent: %+v", l)
		}
		intents = append(intents, l.Data.Intent)
	}
	// Two debug + one debug-choose, in some order.
	debugCount, chooseCount := 0, 0
	for _, i := range intents {
		switch i {
		case IntentDebug:
			debugCount++
		case IntentDebugChoose:
			chooseCount++
		default:
			t.Errorf("unexpected intent: %q", i)
		}
	}
	if debugCount != 2 || chooseCount != 1 {
		t.Errorf("intent mix: got debug=%d choose=%d want 2/1", debugCount, chooseCount)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_CodeLensWithoutPriorParseReturnsEmpty(t *testing.T) {
	// Client asks for lenses on a URI we've never seen. Empty
	// response is the canonical "no lenses for this document."
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
	defer cancel()

	_, done := startServerWithStore(ctx, srv, ServerOptions{})
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})
	var lenses []CodeLens
	if err := conn.Call(ctx, MethodCodeLens, CodeLensParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///nope/gaffer.toml"},
	}, &lenses); err != nil {
		t.Fatalf("codeLens: %v", err)
	}
	if len(lenses) != 0 {
		t.Errorf("expected empty lenses, got %v", lenses)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_DidCloseThenReopenDropsInflightParse(t *testing.T) {
	// Regression: a parse goroutine launched on an OLD version of
	// the buffer (open A) must not land after the buffer has been
	// closed and reopened with new content (open B). Without the
	// staleness check, the in-flight parse would overwrite the
	// fresh state and the codeLens response would reflect the
	// stale parse.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
	defer cancel()

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	n := &notifyCapture{}
	conn := newClientConnCapturing(ctx, cli, n)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})

	_, uri := tempTOMLPath(t)
	bad := `[[projection]]
name = "p"
entry = "p.js"
fixtures.evil = "../escape.json"
`
	good := `[[projection]]
name = "p"
entry = "p.js"
fixtures.happy = "fixtures/happy.json"
`
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: bad},
	})
	waitFor(t, func() bool {
		got := findPublishDiagnostics(n.snapshot(), uri)
		return got != nil && len(got.Diagnostics) == 1
	}, waitForTimeout)

	_ = conn.Notify(ctx, MethodDidClose, &DidCloseTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	})
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: good},
	})
	waitFor(t, func() bool {
		_, ok := server.docs.GetParse(uri)
		return ok
	}, waitForTimeout)

	// The cached parse must reflect the reopened (good) content -
	// no diagnostics, even though the prior open had one.
	parse, _ := server.docs.GetParse(uri)
	diags := emitDiagnostics(parse.Description)
	if len(diags) != 0 {
		t.Errorf("expected reopened parse to be clean, got %d diagnostics: %+v", len(diags), diags)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}
