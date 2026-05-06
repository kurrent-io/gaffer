package lsp

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestServer_CodeLensOnEntryScriptFromCachedToml(t *testing.T) {
	// Open .js file that's the entry of a projection. The lens
	// must come from the cached parse of the matching gaffer.toml,
	// not from any parse of the .js file (we don't parse .js).
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := t.TempDir()
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", `[[projection]]
name = "checkout"
entry = "checkout.js"
fixtures.happy = "fixtures/happy.json"
`)
	tomlURI := pathToURI(cfg)
	jsPath := filepath.Join(root, "checkout.js")
	jsURI := pathToURI(jsPath)

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		WorkspaceFolders: []WorkspaceFolder{{URI: pathToURI(root), Name: "ws"}},
	}, &InitializeResult{})
	_ = conn.Notify(ctx, MethodInitialized, struct{}{})
	waitFor(t, func() bool {
		_, ok := server.docs.GetParse(tomlURI)
		return ok
	}, time.Second)

	var lenses []CodeLens
	if err := conn.Call(ctx, MethodCodeLens, CodeLensParams{
		TextDocument: TextDocumentIdentifier{URI: jsURI},
	}, &lenses); err != nil {
		t.Fatalf("codeLens: %v", err)
	}
	// Projection-level Debug + dropdown for the one valid fixture.
	if len(lenses) != 2 {
		t.Fatalf("expected 2 lenses on entry script, got %d: %+v", len(lenses), lenses)
	}
	intents := map[string]int{}
	for _, l := range lenses {
		if l.Data == nil {
			t.Fatalf("lens missing data: %+v", l)
		}
		intents[l.Data.Intent]++
		// All entry-script lenses anchor at line 0.
		if l.Range.Start.Line != 0 || l.Range.Start.Character != 0 {
			t.Errorf("lens not anchored at line 0: %+v", l.Range)
		}
		// Every lens command's configURI must point at the toml,
		// not the .js file - that's what the editor's debug
		// command consumes.
		if l.Command == nil {
			t.Fatalf("lens missing command: %+v", l)
		}
		args := l.Command.Arguments[0].(map[string]interface{})
		if args["configURI"] != tomlURI {
			t.Errorf("intent %q configURI: got %v want %q", l.Data.Intent, args["configURI"], tomlURI)
		}
	}
	if intents[IntentDebug] != 1 || intents[IntentDebugChoose] != 1 {
		t.Errorf("intent mix: got %v want debug=1 debug-choose=1", intents)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_CodeLensOnNonEntryScriptReturnsEmpty(t *testing.T) {
	// .js file that no projection points at gets zero lenses.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := t.TempDir()
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", `[[projection]]
name = "checkout"
entry = "checkout.js"
`)
	tomlURI := pathToURI(cfg)
	unrelatedURI := pathToURI(filepath.Join(root, "unrelated.js"))

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		WorkspaceFolders: []WorkspaceFolder{{URI: pathToURI(root), Name: "ws"}},
	}, &InitializeResult{})
	_ = conn.Notify(ctx, MethodInitialized, struct{}{})
	waitFor(t, func() bool {
		_, ok := server.docs.GetParse(tomlURI)
		return ok
	}, time.Second)

	var lenses []CodeLens
	if err := conn.Call(ctx, MethodCodeLens, CodeLensParams{
		TextDocument: TextDocumentIdentifier{URI: unrelatedURI},
	}, &lenses); err != nil {
		t.Fatalf("codeLens: %v", err)
	}
	if len(lenses) != 0 {
		t.Errorf("expected zero lenses on unrelated .js, got %+v", lenses)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_CodeLensRefreshFiresOnTomlParse(t *testing.T) {
	// When a gaffer.toml is parsed, the server must push a
	// workspace/codeLens/refresh so any open .js entry-script
	// editor re-fetches its lens. Gated on the client advertising
	// refreshSupport per LSP 3.16 spec.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		Capabilities: ClientCapabilities{
			Workspace: WorkspaceClientCapabilities{
				CodeLens: &CodeLensWorkspaceClientCapabilities{RefreshSupport: true},
			},
		},
	}, &InitializeResult{})
	_, uri := tempTOMLPath(t)
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: `[[projection]]
name = "p"
entry = "p.js"
`},
	})

	waitFor(t, func() bool {
		for _, r := range stub.requestSnapshot() {
			if r.Method == MethodCodeLensRefresh {
				return true
			}
		}
		return false
	}, time.Second)

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_CodeLensRefreshSuppressedWhenClientLacksSupport(t *testing.T) {
	// Spec compliance: clients that don't advertise refreshSupport
	// must not receive workspace/codeLens/refresh requests. Without
	// the gate, every parse would log MethodNotFound from the client.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	// Initialize WITHOUT refreshSupport.
	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})
	_, uri := tempTOMLPath(t)
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: `[[projection]]
name = "p"
entry = "p.js"
`},
	})

	// Wait for the parse to land via its publishDiagnostics so we
	// know the parseAndPublish goroutine completed.
	waitFor(t, func() bool {
		return findPublishDiagnostics(stub.notifSnapshot(), uri) != nil
	}, time.Second)
	// Sleep briefly for any errant refresh to fire (it shouldn't).
	time.Sleep(100 * time.Millisecond)

	for _, r := range stub.requestSnapshot() {
		if r.Method == MethodCodeLensRefresh {
			t.Errorf("refresh fired despite client not advertising refreshSupport")
		}
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_CodeLensOnEntryScriptFromMultipleProjections(t *testing.T) {
	// A single .js file shared as the entry of multiple projections
	// (separate gaffer.tomls or just two projection blocks pointing
	// at the same script) should produce one Debug lens per
	// projection, with the projection name in the title so stacked
	// lenses are distinguishable.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := t.TempDir()
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", `[[projection]]
name = "alpha"
entry = "shared.js"

[[projection]]
name = "beta"
entry = "shared.js"
`)
	tomlURI := pathToURI(cfg)
	jsURI := pathToURI(filepath.Join(root, "shared.js"))

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		WorkspaceFolders: []WorkspaceFolder{{URI: pathToURI(root), Name: "ws"}},
	}, &InitializeResult{})
	_ = conn.Notify(ctx, MethodInitialized, struct{}{})
	waitFor(t, func() bool {
		_, ok := server.docs.GetParse(tomlURI)
		return ok
	}, time.Second)

	var lenses []CodeLens
	if err := conn.Call(ctx, MethodCodeLens, CodeLensParams{
		TextDocument: TextDocumentIdentifier{URI: jsURI},
	}, &lenses); err != nil {
		t.Fatalf("codeLens: %v", err)
	}
	if len(lenses) != 2 {
		t.Fatalf("expected 2 lenses (one per projection sharing entry), got %d: %+v", len(lenses), lenses)
	}
	titles := map[string]bool{}
	for _, l := range lenses {
		if l.Command == nil {
			t.Fatalf("lens missing command: %+v", l)
		}
		titles[l.Command.Title] = true
	}
	if !titles[`Debug "alpha"`] || !titles[`Debug "beta"`] {
		t.Errorf("expected disambiguated titles, got %v", titles)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_CodeLensRefreshFiresOnDidCloseWithCachedParse(t *testing.T) {
	// Closing a buffer that had a cached parse must trigger
	// refresh: any open .js entry-script editor's lens depended
	// on that parse and is now stale.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		Capabilities: ClientCapabilities{
			Workspace: WorkspaceClientCapabilities{
				CodeLens: &CodeLensWorkspaceClientCapabilities{RefreshSupport: true},
			},
		},
	}, &InitializeResult{})
	_, uri := tempTOMLPath(t)
	_ = conn.Notify(ctx, MethodDidOpen, &DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: `[[projection]]
name = "p"
entry = "p.js"
`},
	})
	// Wait for the parse-driven refresh and consume that count.
	waitFor(t, func() bool {
		c := 0
		for _, r := range stub.requestSnapshot() {
			if r.Method == MethodCodeLensRefresh {
				c++
			}
		}
		return c >= 1
	}, time.Second)
	preCloseCount := 0
	for _, r := range stub.requestSnapshot() {
		if r.Method == MethodCodeLensRefresh {
			preCloseCount++
		}
	}

	_ = conn.Notify(ctx, MethodDidClose, &DidCloseTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	})
	waitFor(t, func() bool {
		c := 0
		for _, r := range stub.requestSnapshot() {
			if r.Method == MethodCodeLensRefresh {
				c++
			}
		}
		return c > preCloseCount
	}, time.Second)

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}
