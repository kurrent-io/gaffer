package lsp

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestServer_ProjectionDetailsReturnsConnectionAndFixtures(t *testing.T) {
	// Happy path: returns the project-level connection and the
	// projection's named fixtures. Powers the run-projection
	// picker on the editor side.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := t.TempDir()
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", `[env.local]
connection = "esdb://localhost:2113"
default = true

[[projection]]
name = "checkout"
entry = "checkout.js"
engine_version = 2
fixtures.happy = "fixtures/happy.json"
fixtures.sad = "fixtures/sad.json"
`)
	uri := pathToURI(cfg)
	writeWorkspaceFile(t, root, "checkout.js", "function project(){}")
	writeWorkspaceFile(t, filepath.Join(root, "fixtures"), "happy.json", "[]")
	writeWorkspaceFile(t, filepath.Join(root, "fixtures"), "sad.json", "[]")

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		WorkspaceFolders: []WorkspaceFolder{{URI: pathToURI(root), Name: "ws"}},
	}, &InitializeResult{})
	_ = conn.Notify(ctx, MethodInitialized, struct{}{})
	waitFor(t, func() bool {
		_, ok := server.docs.GetParse(uri)
		return ok
	}, waitForTimeout)

	var result ProjectionDetailsResult
	if err := conn.Call(ctx, MethodProjectionDetails, ProjectionDetailsParams{
		ConfigURI: uri,
		Name:      "checkout",
	}, &result); err != nil {
		t.Fatalf("projectionDetails: %v", err)
	}
	if result.Connection == nil || *result.Connection != "esdb://localhost:2113" {
		t.Errorf("connection: got %v want esdb://localhost:2113", result.Connection)
	}
	// FixtureNames sorts alphabetically so the result is stable.
	if len(result.Fixtures) != 2 || result.Fixtures[0] != "happy" ||
		result.Fixtures[1] != "sad" {
		t.Errorf("fixtures: got %v want [happy sad]", result.Fixtures)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_ProjectionDetailsNilConnectionWhenUndeclared(t *testing.T) {
	// No `connection` field -> Connection comes back as nil (a
	// JSON null), letting the editor gate the "live" option in
	// the picker.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := t.TempDir()
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", `[[projection]]
name = "fixtures-only"
entry = "fixtures-only.js"
fixtures.happy = "fixtures/happy.json"
`)
	uri := pathToURI(cfg)
	writeWorkspaceFile(t, root, "fixtures-only.js", "function project(){}")
	writeWorkspaceFile(t, filepath.Join(root, "fixtures"), "happy.json", "[]")

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		WorkspaceFolders: []WorkspaceFolder{{URI: pathToURI(root), Name: "ws"}},
	}, &InitializeResult{})
	_ = conn.Notify(ctx, MethodInitialized, struct{}{})
	waitFor(t, func() bool {
		_, ok := server.docs.GetParse(uri)
		return ok
	}, waitForTimeout)

	var result ProjectionDetailsResult
	if err := conn.Call(ctx, MethodProjectionDetails, ProjectionDetailsParams{
		ConfigURI: uri,
		Name:      "fixtures-only",
	}, &result); err != nil {
		t.Fatalf("projectionDetails: %v", err)
	}
	if result.Connection != nil {
		t.Errorf("expected nil connection, got %v", *result.Connection)
	}
	if len(result.Fixtures) != 1 || result.Fixtures[0] != "happy" {
		t.Errorf("fixtures: got %v want [happy]", result.Fixtures)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_ProjectionDetailsSkipsMalformedFixtures(t *testing.T) {
	// A fixture with a diagnostic (escape, missing path) is
	// unrunnable. Filtering it out at the source means the editor
	// picker never offers a fixture the CLI would reject.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := t.TempDir()
	// happy points at a real file; missing-path has an empty path
	// which describe() flags as a fixture diagnostic.
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", `[[projection]]
name = "checkout"
entry = "checkout.js"
fixtures.happy = "fixtures/happy.json"
fixtures.broken = ""
`)
	uri := pathToURI(cfg)
	writeWorkspaceFile(t, root, "checkout.js", "function project(){}")
	writeWorkspaceFile(t, filepath.Join(root, "fixtures"), "happy.json", "[]")

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		WorkspaceFolders: []WorkspaceFolder{{URI: pathToURI(root), Name: "ws"}},
	}, &InitializeResult{})
	_ = conn.Notify(ctx, MethodInitialized, struct{}{})
	waitFor(t, func() bool {
		_, ok := server.docs.GetParse(uri)
		return ok
	}, waitForTimeout)

	var result ProjectionDetailsResult
	if err := conn.Call(ctx, MethodProjectionDetails, ProjectionDetailsParams{
		ConfigURI: uri,
		Name:      "checkout",
	}, &result); err != nil {
		t.Fatalf("projectionDetails: %v", err)
	}
	if len(result.Fixtures) != 1 || result.Fixtures[0] != "happy" {
		t.Errorf("fixtures: got %v want [happy] (broken should be filtered)", result.Fixtures)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_ProjectionDetailsEmptyResultForUnknownConfigURI(t *testing.T) {
	// Unknown URI -> empty result rather than an error so the
	// editor falls through to the live-only flow without surfacing
	// a toast.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := startServer(ctx, srv, ServerOptions{})
	conn := newClientConn(ctx, cli)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{}, &InitializeResult{})

	var result ProjectionDetailsResult
	if err := conn.Call(ctx, MethodProjectionDetails, ProjectionDetailsParams{
		ConfigURI: "file:///nope/gaffer.toml",
		Name:      "x",
	}, &result); err != nil {
		t.Fatalf("projectionDetails: %v", err)
	}
	if result.Connection != nil {
		t.Errorf("expected nil connection, got %v", *result.Connection)
	}
	if len(result.Fixtures) != 0 {
		t.Errorf("expected empty fixtures, got %v", result.Fixtures)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestServer_ProjectionDetailsEmptyResultForUnknownProjection(t *testing.T) {
	// Known config URI but the projection name doesn't match -
	// same fall-through as unknown URI; editor degrades to live.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := t.TempDir()
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", `[env.local]
connection = "esdb://localhost:2113"
default = true

[[projection]]
name = "real"
entry = "real.js"
engine_version = 2
`)
	uri := pathToURI(cfg)
	writeWorkspaceFile(t, root, "real.js", "function project(){}")

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	stub := &clientStub{}
	conn := newClientConnStub(ctx, cli, stub)
	defer func() { _ = conn.Close() }()

	_ = conn.Call(ctx, MethodInitialize, &InitializeParams{
		WorkspaceFolders: []WorkspaceFolder{{URI: pathToURI(root), Name: "ws"}},
	}, &InitializeResult{})
	_ = conn.Notify(ctx, MethodInitialized, struct{}{})
	waitFor(t, func() bool {
		_, ok := server.docs.GetParse(uri)
		return ok
	}, waitForTimeout)

	var result ProjectionDetailsResult
	if err := conn.Call(ctx, MethodProjectionDetails, ProjectionDetailsParams{
		ConfigURI: uri,
		Name:      "imaginary",
	}, &result); err != nil {
		t.Fatalf("projectionDetails: %v", err)
	}
	if result.Connection != nil {
		t.Errorf("expected nil connection on unknown-projection fall-through, got %v", *result.Connection)
	}
	if len(result.Fixtures) != 0 {
		t.Errorf("expected empty fixtures, got %v", result.Fixtures)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}
