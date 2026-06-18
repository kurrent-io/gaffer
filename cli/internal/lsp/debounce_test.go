package lsp

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestServer_DidChangeIsDebounced(t *testing.T) {
	// A burst of didChanges arrives faster than the debounce
	// window. Only ONE publishDiagnostics should fire, reflecting
	// the latest text - intermediate keystrokes (which may be
	// transiently invalid) must not flicker squiggles.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
	defer cancel()

	_, done := startServerWithStore(ctx, srv, ServerOptions{
		DebounceWindow: 200 * time.Millisecond,
	})
	n := &notifyCapture{}
	conn := newClientConnCapturing(ctx, cli, n)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})

	_, uri := tempTOMLPath(t)
	final := `[[projection]]
name = "p"
entry = "p.js"
fixtures.happy = "fixtures/happy.json"
`
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: "engine_version = 2"},
	})
	// Wait for the didOpen parse to land first so we can count
	// only the didChange publishes after this point.
	waitFor(t, func() bool {
		return findPublishDiagnostics(n.snapshot(), uri) != nil
	}, waitForTimeout)
	baseline := countPublishDiagnostics(n.snapshot(), uri)

	// Five rapid changes well inside one debounce window. Each
	// resets the timer; only the final one should produce a
	// publish.
	for i, text := range []string{
		`fixtures.evi`,
		`fixtures.evil = "../escape.json"`,
		`[[projection]]
name = "p"`,
		`[[projection]]
name = "p"
entry = "p.js"`,
		final,
	} {
		_ = conn.Notify(ctx, MethodDidChange, &DidChangeTextDocumentParams{
			TextDocument:   VersionedTextDocumentIdentifier{URI: uri, Version: i + 2},
			ContentChanges: []TextDocumentContentChangeEvent{{Text: text}},
		})
		time.Sleep(20 * time.Millisecond) // 5x20=100ms total, well under 200ms window
	}

	// Wait long enough for the post-burst debounce to fire. Count
	// publishes since baseline - findPublishDiagnostics's "latest"
	// could be satisfied by the didOpen publish itself.
	waitFor(t, func() bool {
		return countPublishDiagnostics(n.snapshot(), uri)-baseline >= 1
	}, waitForTimeout)

	// Exactly one new publish should have arrived since baseline -
	// the intermediate transient states must not have surfaced.
	after := countPublishDiagnostics(n.snapshot(), uri)
	if got := after - baseline; got != 1 {
		t.Errorf("expected 1 debounced publish, got %d", got)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_DidCloseClearsPendingDebounce(t *testing.T) {
	// didChange arms a debounce; before it fires, didClose lands.
	// The pending parse must NOT run - publishing diagnostics for
	// a buffer the user just closed would leave squiggles in the
	// editor's Problems panel.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
	defer cancel()

	_, done := startServerWithStore(ctx, srv, ServerOptions{
		DebounceWindow: 200 * time.Millisecond,
	})
	n := &notifyCapture{}
	conn := newClientConnCapturing(ctx, cli, n)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})

	_, uri := tempTOMLPath(t)
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: "engine_version = 2"},
	})
	// Wait for the didOpen publish so it doesn't pollute the
	// post-close count.
	waitFor(t, func() bool {
		return findPublishDiagnostics(n.snapshot(), uri) != nil
	}, waitForTimeout)

	// didChange to invalid content; didClose immediately (well
	// under the debounce window).
	_ = conn.Notify(ctx, MethodDidChange, &DidChangeTextDocumentParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: 2},
		ContentChanges: []TextDocumentContentChangeEvent{{Text: `[[projection]]
name = "p"
entry = "p.js"
fixtures.evil = "../escape.json"
`}},
	})
	_ = conn.Notify(ctx, MethodDidClose, &DidCloseTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	})

	// didClose publishes empty diagnostics synchronously. Wait
	// past the debounce window and confirm no second publish with
	// non-empty diagnostics ever arrived.
	waitFor(t, func() bool {
		got := findPublishDiagnostics(n.snapshot(), uri)
		return got != nil && len(got.Diagnostics) == 0
	}, 500*time.Millisecond)
	time.Sleep(300 * time.Millisecond) // > debounce window

	// Assert: no publish with non-empty diagnostics ever fired
	// between didChange and now.
	for _, c := range n.snapshot() {
		if c.Method != MethodPublishDiagnostics {
			continue
		}
		var p PublishDiagnosticsParams
		if err := json.Unmarshal(c.Params, &p); err != nil || p.URI != uri {
			continue
		}
		if len(p.Diagnostics) != 0 {
			t.Errorf("unexpected late parse fired: %+v", p.Diagnostics)
		}
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_ShutdownDrainsPendingDebounce(t *testing.T) {
	// A debounce timer pending when Run returns must not fire its
	// parse + publish post-shutdown. With a 1s window we shutdown
	// well before the timer would normally fire; the drain in
	// Run's defer plus the callback's identity check together
	// prevent any late publish.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
	defer cancel()

	_, done := startServerWithStore(ctx, srv, ServerOptions{
		DebounceWindow: time.Second,
	})
	n := &notifyCapture{}
	conn := newClientConnCapturing(ctx, cli, n)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})
	_, uri := tempTOMLPath(t)
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: "engine_version = 2"},
	})
	waitFor(t, func() bool {
		return findPublishDiagnostics(n.snapshot(), uri) != nil
	}, waitForTimeout)
	baseline := countPublishDiagnostics(n.snapshot(), uri)

	_ = conn.Notify(ctx, MethodDidChange, &DidChangeTextDocumentParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: 2},
		ContentChanges: []TextDocumentContentChangeEvent{{Text: `[[projection]]
name = "p"
entry = "p.js"
fixtures.evil = "../escape.json"
`}},
	})
	// Shutdown well before the 1s window would have elapsed.
	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done

	// Wait past when the timer would have fired and confirm no
	// further publish landed for our URI.
	time.Sleep(1100 * time.Millisecond)
	if got := countPublishDiagnostics(n.snapshot(), uri) - baseline; got != 0 {
		t.Errorf("expected 0 publishes after shutdown, got %d", got)
	}
}

func TestServer_DidChangeWatchedFilesIgnoresOpenBufferDebounce(t *testing.T) {
	// With a buffer open and a debounce pending, a watched-file
	// Changed event must not race the buffer's debounced parse to
	// install disk content. AddFromDisk is a no-op when memory-
	// sourced, so the buffer survives. Pin the contract.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
	defer cancel()

	root := t.TempDir()
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", "DISK-ORIGINAL")
	uri := pathToURI(cfg)

	server, done := startServerWithStore(ctx, srv, ServerOptions{
		DebounceWindow: 200 * time.Millisecond,
	})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})

	memContent := `[[projection]]
name = "p"
entry = "p.js"
fixtures.happy = "fixtures/happy.json"
`
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: memContent},
	})
	waitFor(t, func() bool {
		state, ok := server.docs.Get(uri)
		return ok && state.Source == sourceMemory
	}, waitForTimeout)

	// Arm a debounce that will eventually parse the buffer.
	_ = conn.Notify(ctx, MethodDidChange, &DidChangeTextDocumentParams{
		TextDocument: VersionedTextDocumentIdentifier{URI: uri, Version: 2},
		ContentChanges: []TextDocumentContentChangeEvent{{Text: `[[projection]]
name = "p"
entry = "p.js"
fixtures.evil = "../escape.json"
`}},
	})
	// Disk-side write while debounce pending.
	if err := os.WriteFile(cfg, []byte("DISK-OVERWRITE"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.Notify(ctx, MethodDidChangeWatchedFiles, &DidChangeWatchedFilesParams{
		Changes: []FileEvent{{URI: uri, Type: FileChangeChanged}},
	})

	// Wait past the debounce window so any racing publish has
	// landed.
	time.Sleep(400 * time.Millisecond)
	state, ok := server.docs.Get(uri)
	if !ok {
		t.Fatal("expected URI to remain in store")
	}
	if state.Source != sourceMemory {
		t.Errorf("source: got %v want sourceMemory", state.Source)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_DidChangeDebounceIsPerURI(t *testing.T) {
	// Bursting on URI A must NOT delay URI B's parse. Per-URI
	// keying is the contract - a future "global debounce" refactor
	// would break it loudly.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
	defer cancel()

	_, done := startServerWithStore(ctx, srv, ServerOptions{
		DebounceWindow: 200 * time.Millisecond,
	})
	n := &notifyCapture{}
	conn := newClientConnCapturing(ctx, cli, n)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})

	_, uriA := tempTOMLPath(t)
	_, uriB := tempTOMLPath(t)
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uriA, Text: "engine_version = 2"},
	})
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uriB, Text: "engine_version = 2"},
	})
	waitFor(t, func() bool {
		return findPublishDiagnostics(n.snapshot(), uriA) != nil &&
			findPublishDiagnostics(n.snapshot(), uriB) != nil
	}, waitForTimeout)
	baselineA := countPublishDiagnostics(n.snapshot(), uriA)
	baselineB := countPublishDiagnostics(n.snapshot(), uriB)

	// Burst URI A but leave URI B alone. After A's debounce
	// fires, both should have advanced by exactly one - A from
	// the burst, B from its single change.
	for i := 0; i < 5; i++ {
		_ = conn.Notify(ctx, MethodDidChange, &DidChangeTextDocumentParams{
			TextDocument:   VersionedTextDocumentIdentifier{URI: uriA, Version: i + 2},
			ContentChanges: []TextDocumentContentChangeEvent{{Text: "engine_version = 2"}},
		})
		time.Sleep(20 * time.Millisecond)
	}
	_ = conn.Notify(ctx, MethodDidChange, &DidChangeTextDocumentParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uriB, Version: 2},
		ContentChanges: []TextDocumentContentChangeEvent{{Text: "engine_version = 2"}},
	})

	waitFor(t, func() bool {
		return countPublishDiagnostics(n.snapshot(), uriA)-baselineA >= 1 &&
			countPublishDiagnostics(n.snapshot(), uriB)-baselineB >= 1
	}, waitForTimeout)

	// Each URI should have produced exactly one debounced publish.
	if got := countPublishDiagnostics(n.snapshot(), uriA) - baselineA; got != 1 {
		t.Errorf("URI A: expected 1 debounced publish, got %d", got)
	}
	if got := countPublishDiagnostics(n.snapshot(), uriB) - baselineB; got != 1 {
		t.Errorf("URI B: expected 1 debounced publish, got %d", got)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_DidOpenIsImmediate(t *testing.T) {
	// didOpen must publish well before the debounce window - it's
	// the first parse, not a keystroke. The 250ms waitFor below sits
	// under the 500ms debounce window, so a didOpen wrongly routed
	// through the debouncer would miss that window and fail.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
	defer cancel()

	_, done := startServerWithStore(ctx, srv, ServerOptions{
		DebounceWindow: 500 * time.Millisecond,
	})
	n := &notifyCapture{}
	conn := newClientConnCapturing(ctx, cli, n)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})

	_, uri := tempTOMLPath(t)
	start := time.Now()
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: "engine_version = 2"},
	})
	waitFor(t, func() bool {
		return findPublishDiagnostics(n.snapshot(), uri) != nil
	}, 250*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Errorf("didOpen should publish promptly, took %v", elapsed)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}
