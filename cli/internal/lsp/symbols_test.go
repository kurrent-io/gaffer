package lsp

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestServer_WorkspaceSymbolReturnsProjectionsAcrossTomls(t *testing.T) {
	// workspace/symbol must aggregate projections from every
	// cached parse, not just the one matching the request URI.
	// Powers the editor's QuickPick and Cmd+T navigation.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := t.TempDir()
	cfgA := writeWorkspaceFile(t, filepath.Join(root, "a"), "gaffer.toml", `[[projection]]
name = "alpha"
entry = "alpha.js"
`)
	cfgB := writeWorkspaceFile(t, filepath.Join(root, "b"), "gaffer.toml", `[[projection]]
name = "beta"
entry = "beta.js"
`)
	uriA := pathToURI(cfgA)
	uriB := pathToURI(cfgB)

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		WorkspaceFolders: []WorkspaceFolder{{URI: pathToURI(root), Name: "ws"}},
	}, &InitializeResult{})
	_ = conn.Notify(ctx, MethodInitialized, struct{}{})
	waitFor(t, func() bool {
		_, okA := server.docs.GetParse(uriA)
		_, okB := server.docs.GetParse(uriB)
		return okA && okB
	}, waitForTimeout)

	var symbols []SymbolInformation
	if err := conn.Call(ctx, MethodWorkspaceSymbol, WorkspaceSymbolParams{}, &symbols); err != nil {
		t.Fatalf("workspace/symbol: %v", err)
	}
	if len(symbols) != 2 {
		t.Fatalf("expected 2 symbols, got %d: %+v", len(symbols), symbols)
	}
	names := map[string]bool{}
	for _, s := range symbols {
		names[s.Name] = true
		if s.Kind != SymbolKindFunction {
			t.Errorf("symbol %q kind: got %d want SymbolKindFunction", s.Name, s.Kind)
		}
		if s.ContainerName != "gaffer.toml" {
			t.Errorf("symbol %q container: got %q want gaffer.toml", s.Name, s.ContainerName)
		}
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("expected both alpha and beta symbols, got %v", names)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_WorkspaceSymbolSkipsInvalidProjections(t *testing.T) {
	// Projections with a header-level diagnostic (missing name,
	// missing entry, escape) aren't actionable - a navigation
	// target for them would lead nowhere useful. Skip them.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := t.TempDir()
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", `[[projection]]
name = "good"
entry = "good.js"

[[projection]]
entry = "missing-name.js"
`)
	tomlURI := pathToURI(cfg)

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
	}, waitForTimeout)

	var symbols []SymbolInformation
	if err := conn.Call(ctx, MethodWorkspaceSymbol, WorkspaceSymbolParams{}, &symbols); err != nil {
		t.Fatalf("workspace/symbol: %v", err)
	}
	if len(symbols) != 1 || symbols[0].Name != "good" {
		t.Errorf("expected only 'good' symbol, got %+v", symbols)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_WorkspaceSymbolCapabilityAdvertised(t *testing.T) {
	// Pin the capability surface - clients gate Cmd+T on this.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := startServer(ctx, srv, ServerOptions{})
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()

	var result InitializeResult
	if err := conn.Call(ctx, MethodInitialize, &InitializeParams{}, &result); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if result.Capabilities.WorkspaceSymbolProvider == nil {
		t.Error("server did not advertise WorkspaceSymbolProvider")
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}
