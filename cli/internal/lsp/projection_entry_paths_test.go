package lsp

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestServer_ProjectionEntryPathsReturnsAllValidEntries(t *testing.T) {
	// Happy path across two tomls in different directories. Returns
	// a sorted, deduplicated list of absolute entry paths. Powers the
	// VS Code tsserver plugin's projection-files allowlist.
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

[[projection]]
name = "gamma"
entry = "gamma.js"
`)
	writeWorkspaceFile(t, filepath.Join(root, "a"), "alpha.js", "function project(){}")
	writeWorkspaceFile(t, filepath.Join(root, "b"), "beta.js", "function project(){}")
	writeWorkspaceFile(t, filepath.Join(root, "b"), "gamma.js", "function project(){}")

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		WorkspaceFolders: []WorkspaceFolder{{URI: pathToURI(root), Name: "ws"}},
	}, &InitializeResult{})
	_ = conn.Notify(ctx, MethodInitialized, struct{}{})
	waitFor(t, func() bool {
		_, okA := server.docs.GetParse(pathToURI(cfgA))
		_, okB := server.docs.GetParse(pathToURI(cfgB))
		return okA && okB
	}, time.Second)

	var result ProjectionEntryPathsResult
	if err := conn.Call(ctx, MethodProjectionEntryPaths, struct{}{}, &result); err != nil {
		t.Fatalf("projectionEntryPaths: %v", err)
	}
	wantAlpha := filepath.Join(root, "a", "alpha.js")
	wantBeta := filepath.Join(root, "b", "beta.js")
	wantGamma := filepath.Join(root, "b", "gamma.js")
	want := []string{wantAlpha, wantBeta, wantGamma}
	if len(result.Paths) != len(want) {
		t.Fatalf("paths: got %d (%v), want %d (%v)", len(result.Paths), result.Paths, len(want), want)
	}
	for i, p := range want {
		if result.Paths[i] != p {
			t.Errorf("paths[%d]: got %q, want %q", i, result.Paths[i], p)
		}
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_ProjectionEntryPathsSkipsInvalidProjections(t *testing.T) {
	// A projection with a header-level diagnostic (here: missing
	// name) doesn't make it through validProjections, and its entry
	// path is excluded. Otherwise the plugin would try to inject
	// types into a file the runtime never runs.
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
	writeWorkspaceFile(t, root, "good.js", "function project(){}")
	writeWorkspaceFile(t, root, "missing-name.js", "function project(){}")

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		WorkspaceFolders: []WorkspaceFolder{{URI: pathToURI(root), Name: "ws"}},
	}, &InitializeResult{})
	_ = conn.Notify(ctx, MethodInitialized, struct{}{})
	waitFor(t, func() bool {
		_, ok := server.docs.GetParse(pathToURI(cfg))
		return ok
	}, time.Second)

	var result ProjectionEntryPathsResult
	if err := conn.Call(ctx, MethodProjectionEntryPaths, struct{}{}, &result); err != nil {
		t.Fatalf("projectionEntryPaths: %v", err)
	}
	if len(result.Paths) != 1 || result.Paths[0] != filepath.Join(root, "good.js") {
		t.Errorf("paths: got %v, want exactly [%q]", result.Paths, filepath.Join(root, "good.js"))
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_ProjectionEntryPathsEmptyWhenNoWorkspace(t *testing.T) {
	// No workspace folders -> no walk -> no parses -> empty result.
	// Pinned so the response shape stays a real empty array (not
	// null) for client-side iteration.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := startServer(ctx, srv, ServerOptions{})
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})

	var result ProjectionEntryPathsResult
	if err := conn.Call(ctx, MethodProjectionEntryPaths, struct{}{}, &result); err != nil {
		t.Fatalf("projectionEntryPaths: %v", err)
	}
	if result.Paths == nil {
		t.Errorf("expected empty slice, got nil")
	}
	if len(result.Paths) != 0 {
		t.Errorf("expected empty slice, got %v", result.Paths)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}
