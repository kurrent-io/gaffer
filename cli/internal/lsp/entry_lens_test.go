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
	}
	if intents[IntentDebug] != 1 || intents[IntentDebugChoose] != 1 {
		t.Errorf("intent mix: got %v want debug=1 debug-choose=1", intents)
	}
	// Lens command's configURI must point at the toml, not the
	// .js file - that's what the editor's debug command consumes.
	args := lenses[0].Command.Arguments[0].(map[string]interface{})
	if args["configURI"] != tomlURI {
		t.Errorf("configURI: got %v want %q", args["configURI"], tomlURI)
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
	// editor re-fetches its lens.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})
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
