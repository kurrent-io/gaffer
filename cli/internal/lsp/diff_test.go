package lsp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/target"
)

const diffConfig = `[env.local]
connection = "esdb://localhost:2113"
default = true

[[projection]]
name = "checkout"
entry = "checkout.js"
engine_version = 2
`

// seedDiffServer builds a status-lens-capable server with the diff config opened
// and the given fake diff fetcher wired in.
func seedDiffServer(t *testing.T, fetch diffFetchFunc) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	cfg := writeWorkspaceFile(t, root, "gaffer.toml", diffConfig)
	writeWorkspaceFile(t, root, "checkout.js", "function project(){}")
	uri := pathToURI(cfg)
	s := testServer(nil)
	s.diffFetch = fetch
	s.docs.Open(uri, diffConfig)
	return s, uri
}

func diffReq(t *testing.T, uri, name, env string) *jsonrpc2.Request {
	t.Helper()
	req := &jsonrpc2.Request{}
	if err := req.SetParams(DiffProjectionParams{ConfigURI: uri, Name: name, Env: env}); err != nil {
		t.Fatal(err)
	}
	return req
}

// failDiffFetch is a fetcher that fails the test if the handler reaches it - used
// by cases that should be rejected before any read.
func failDiffFetch(t *testing.T) diffFetchFunc {
	t.Helper()
	return func(context.Context, string, *config.Config, string, string, string) (cliout.DiffJSON, *jsonrpc2.Error) {
		t.Error("diffFetch should not be reached")
		return cliout.DiffJSON{}, nil
	}
}

func assertJSONRPCCode(t *testing.T, err error, want int64) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a *jsonrpc2.Error with code %d, got nil", want)
	}
	var je *jsonrpc2.Error
	if !errors.As(err, &je) {
		t.Fatalf("expected *jsonrpc2.Error, got %T: %v", err, err)
	}
	if je.Code != want {
		t.Fatalf("code: got %d want %d (%s)", je.Code, want, je.Message)
	}
}

func TestHandleDiffProjection_ReturnsFetchResult(t *testing.T) {
	want := cliout.DiffJSON{
		Name:  "checkout",
		Left:  cliout.DiffSideJSON{Ref: "deployed", Source: "a"},
		Right: cliout.DiffSideJSON{Ref: "local", Source: "b"},
	}
	var gotURI, gotEnv, gotName string
	s, uri := seedDiffServer(t, func(_ context.Context, _ string, _ *config.Config, u, env, name string) (cliout.DiffJSON, *jsonrpc2.Error) {
		gotURI, gotEnv, gotName = u, env, name
		return want, nil
	})

	got, err := s.handleDiffProjection(context.Background(), diffReq(t, uri, "checkout", "local"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dj, ok := got.(cliout.DiffJSON)
	if !ok {
		t.Fatalf("expected cliout.DiffJSON, got %T (%v)", got, got)
	}
	if dj.Name != "checkout" || dj.Left.Source != "a" || dj.Right.Source != "b" {
		t.Errorf("payload: %+v", dj)
	}
	if gotURI != uri || gotEnv != "local" || gotName != "checkout" {
		t.Errorf("fetch args: uri=%q env=%q name=%q", gotURI, gotEnv, gotName)
	}
}

func TestHandleDiffProjection_AuthErrorPassesThrough(t *testing.T) {
	s, uri := seedDiffServer(t, func(context.Context, string, *config.Config, string, string, string) (cliout.DiffJSON, *jsonrpc2.Error) {
		return cliout.DiffJSON{}, authRequiredError("local")
	})
	_, err := s.handleDiffProjection(context.Background(), diffReq(t, uri, "checkout", "local"))
	assertJSONRPCCode(t, err, CodeAuthRequired)
}

func TestHandleDiffProjection_GenericErrorPassesThrough(t *testing.T) {
	s, uri := seedDiffServer(t, func(context.Context, string, *config.Config, string, string, string) (cliout.DiffJSON, *jsonrpc2.Error) {
		return cliout.DiffJSON{}, &jsonrpc2.Error{Code: jsonrpc2.CodeInternalError, Message: "boom"}
	})
	_, err := s.handleDiffProjection(context.Background(), diffReq(t, uri, "checkout", "local"))
	assertJSONRPCCode(t, err, jsonrpc2.CodeInternalError)
}

func TestHandleDiffProjection_NilParams(t *testing.T) {
	s, _ := seedDiffServer(t, failDiffFetch(t))
	_, err := s.handleDiffProjection(context.Background(), &jsonrpc2.Request{})
	assertJSONRPCCode(t, err, jsonrpc2.CodeInvalidParams)
}

func TestHandleDiffProjection_NoConfigForURI(t *testing.T) {
	s, _ := seedDiffServer(t, failDiffFetch(t))
	_, err := s.handleDiffProjection(context.Background(), diffReq(t, "file:///nope/gaffer.toml", "checkout", "local"))
	assertJSONRPCCode(t, err, jsonrpc2.CodeInvalidParams)
}

func TestHandleDiffProjection_ServedWithoutStatusLens(t *testing.T) {
	// diffProjection is a client-pulled request, so it must not be gated on the
	// vscode-oriented statusLens rendering capability - a client that never
	// claimed statusLens can still ask for a diff.
	reached := false
	s, uri := seedDiffServer(t, func(context.Context, string, *config.Config, string, string, string) (cliout.DiffJSON, *jsonrpc2.Error) {
		reached = true
		return cliout.DiffJSON{Name: "checkout"}, nil
	})
	s.statusLensCapable = false
	if _, err := s.handleDiffProjection(context.Background(), diffReq(t, uri, "checkout", "local")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reached {
		t.Error("diff should be served without the statusLens capability")
	}
}

func TestDiffProjection_EndToEndOverConn(t *testing.T) {
	// The full request path over a real jsonrpc2 conn: offloadBlocking -> spawn
	// -> HandlerWithError -> dispatch -> reply. No statusLens is claimed, so this
	// also pins that the diff is served without that capability.
	srv, cli := pipePair()
	ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
	defer cancel()

	root := t.TempDir()
	cfgPath := writeWorkspaceFile(t, root, "gaffer.toml", diffConfig)
	writeWorkspaceFile(t, root, "checkout.js", "function project(){}")
	uri := pathToURI(cfgPath)

	server, done := startServerWithStore(ctx, srv, ServerOptions{})
	// Set before any request is sent; the diffProjection Call below provides the
	// happens-before to the handler goroutine that reads it.
	server.diffFetch = func(_ context.Context, _ string, _ *config.Config, _, _, name string) (cliout.DiffJSON, *jsonrpc2.Error) {
		return cliout.DiffJSON{
			Name:  name,
			Left:  cliout.DiffSideJSON{Ref: "deployed", Source: "OLD"},
			Right: cliout.DiffSideJSON{Ref: "local", Source: "NEW"},
		}, nil
	}

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

	var result cliout.DiffJSON
	if err := conn.Call(ctx, MethodDiffProjection, DiffProjectionParams{
		ConfigURI: uri, Name: "checkout", Env: "local",
	}, &result); err != nil {
		t.Fatalf("diffProjection: %v", err)
	}
	if result.Left.Source != "OLD" || result.Right.Source != "NEW" {
		t.Errorf("diff payload round-trip: %+v", result)
	}

	_ = conn.Call(ctx, MethodShutdown, nil, nil)
	_ = conn.Notify(ctx, MethodExit, nil)
	<-done
}

func TestDialError(t *testing.T) {
	authErr := &target.AuthRequiredError{Env: "prod"}
	if je := dialError(authErr, "prod"); je.Code != CodeAuthRequired {
		t.Errorf("bare AuthRequiredError: code %d want %d", je.Code, CodeAuthRequired)
	}
	// errors.As unwraps, so a wrapped auth error still classifies as sign-in.
	if je := dialError(fmt.Errorf("dial: %w", authErr), "prod"); je.Code != CodeAuthRequired {
		t.Errorf("wrapped AuthRequiredError: code %d want %d", je.Code, CodeAuthRequired)
	}
	if je := dialError(errors.New("connection refused"), "prod"); je.Code != jsonrpc2.CodeInternalError {
		t.Errorf("generic dial error: code %d want %d", je.Code, jsonrpc2.CodeInternalError)
	}
}

func TestGuardedOp_PassesThroughResult(t *testing.T) {
	cfg, err := config.Parse([]byte("[env.prod]\nconnection = \"esdb://host:2113\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	got, gerr := guardedOp(cfg, "/root", "prod", "diff", func() (drift.Comparison, error) {
		return drift.Comparison{Name: "checkout", State: drift.InSync}, nil
	})
	if gerr != nil || got.Name != "checkout" {
		t.Fatalf("got %+v, %v", got, gerr)
	}
}

// TestGuardedOp_RecoversAndScrubsPanic covers what the integration test can't: a
// crash deep in the op is recovered into a generic error (the op runs off the
// read loop, so an unrecovered panic would be fatal), and the ${VAR}-expanded
// connection secret the panic carries is scrubbed from the log.
func TestGuardedOp_RecoversAndScrubsPanic(t *testing.T) {
	t.Setenv("DIFF_TEST_PW", "s3cr3t")
	cfg, err := config.Parse([]byte("[env.prod]\nconnection = \"kurrentdb://user:${DIFF_TEST_PW}@host:2113\"\n"))
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })

	_, gerr := guardedOp(cfg, t.TempDir(), "prod", "diff", func() (drift.Comparison, error) {
		panic("dial failed: kurrentdb://user:s3cr3t@host:2113")
	})
	if gerr == nil {
		t.Fatal("a panic should surface as an error, not crash")
	}
	if strings.Contains(gerr.Error(), "s3cr3t") {
		t.Errorf("error leaks the panic value: %v", gerr)
	}
	if strings.Contains(buf.String(), "s3cr3t") {
		t.Errorf("expanded connection secret leaked into the panic log: %q", buf.String())
	}
}

// funcHandler adapts a func to jsonrpc2.Handler for the offload wrapper tests.
type funcHandler func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request)

func (f funcHandler) Handle(ctx context.Context, c *jsonrpc2.Conn, r *jsonrpc2.Request) {
	f(ctx, c, r)
}

func TestBlocksReadLoop(t *testing.T) {
	for _, m := range []string{MethodDiffProjection, MethodOperateProjection} {
		if !blocksReadLoop(m) {
			t.Errorf("%s does blocking network I/O and must run off the read loop", m)
		}
	}
	for _, m := range []string{MethodHover, MethodCodeLens, MethodProjectionDetails, MethodRefreshStatus, MethodDidChange} {
		if blocksReadLoop(m) {
			t.Errorf("%s should stay inline on the read loop", m)
		}
	}
}

// TestBlockingMethodsAreDispatched guards against a blockingMethods entry that
// handle's switch doesn't actually dispatch (a typo or a removed case): such a
// method would be offloaded but then answered with MethodNotFound. It can't catch
// the opposite drift - a blocking handler added to the switch but not to
// blockingMethods - which the comment on blockingMethods flags instead.
func TestBlockingMethodsAreDispatched(t *testing.T) {
	s := NewServer(ServerOptions{})
	for m := range blockingMethods {
		_, err := s.handle(context.Background(), nil, &jsonrpc2.Request{Method: m})
		var je *jsonrpc2.Error
		if errors.As(err, &je) && je.Code == jsonrpc2.CodeMethodNotFound {
			t.Errorf("blocking method %q is not dispatched by handle's switch", m)
		}
	}
}

// goSpawn is a test stand-in for Server.spawn: run the work in a goroutine and
// report that it was queued.
func goSpawn(fn func()) bool {
	go fn()
	return true
}

func TestOffloadBlocking_RunsBlockingRequestOffTheReadLoop(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	h := offloadBlocking(funcHandler(func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) {
		close(entered)
		<-release
	}), goSpawn)

	returned := make(chan struct{})
	go func() {
		h.Handle(context.Background(), nil, &jsonrpc2.Request{Method: MethodDiffProjection})
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("Handle should return before the blocking handler completes")
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the blocking handler should have started in its own goroutine")
	}
	close(release)
}

func TestOffloadBlocking_RunsNonBlockingInline(t *testing.T) {
	ran := false
	spawned := false
	h := offloadBlocking(funcHandler(func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) {
		ran = true
	}), func(fn func()) bool { spawned = true; go fn(); return true })
	// A non-blocking method runs inline, so it has completed by the time Handle
	// returns - no goroutine, no synchronisation needed to observe the write.
	h.Handle(context.Background(), nil, &jsonrpc2.Request{Method: MethodHover})
	if !ran {
		t.Error("a non-blocking method should run inline")
	}
	if spawned {
		t.Error("a non-blocking method must not be offloaded")
	}
}

func TestOffloadBlocking_RunsInlineWhenSpawnRefused(t *testing.T) {
	ran := false
	// spawn refuses (Run is draining); the handler must still run - inline - so
	// the client gets a reply instead of hanging.
	h := offloadBlocking(funcHandler(func(context.Context, *jsonrpc2.Conn, *jsonrpc2.Request) {
		ran = true
	}), func(func()) bool { return false })
	h.Handle(context.Background(), nil, &jsonrpc2.Request{Method: MethodDiffProjection})
	if !ran {
		t.Error("a refused spawn should fall back to running inline")
	}
}
