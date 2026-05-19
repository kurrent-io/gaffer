package cmd

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry/telemetrytest"
)

// testIdentity is a fixed deterministic identity injected into Clients
// built by cmd-package integration tests. Cobra tests don't go through
// StartupGate, so they use telemetry.WithIdentity to stamp envelopes
// without minting.
var testIdentity = telemetry.Identity{
	TelemetryID: "11111111-1111-1111-1111-111111111111",
	Salt:        "22222222-2222-2222-2222-222222222222",
	RunID:       "33333333-3333-3333-3333-333333333333",
}

// runCmdWithTelemetry constructs a fresh root with a Client wired to
// the supplied mock sink on its ctx, runs the given args, then Flushes
// the Client so the mock can be inspected synchronously. Returns the
// cobra error so tests can assert on it.
//
// Flush has to happen before the caller reads mock.Envelopes() because
// Client.emit is async (one goroutine per envelope). Production main
// does the same drain at exit; tests just bring it forward.
func runCmdWithTelemetry(t *testing.T, mock *telemetrytest.MockSink, args ...string) error {
	t.Helper()
	// Hermetic baseline. The first group (CI / TEAMCITY_VERSION /
	// JENKINS_URL / GAFFER_TELEMETRY_DEBUG) is what telemetry.New
	// reads at construction - cleared so tests don't inherit those
	// from the runner. The second group (opt-out vars) doesn't
	// affect this test path today (we bypass StartupGate by
	// constructing the Client directly), but clearing them guards
	// against a future refactor that routes through StartupGate
	// silently changing test behaviour.
	for _, k := range []string{
		"CI", "TEAMCITY_VERSION", "JENKINS_URL", "GAFFER_TELEMETRY_DEBUG",
		"GAFFER_TELEMETRY_OPTOUT", "KURRENTDB_TELEMETRY_OPTOUT", "DO_NOT_TRACK",
	} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
	c := telemetry.New(
		telemetry.WithSink(mock),
		telemetry.WithIdentity(testIdentity),
	)
	ctx := telemetry.WithClient(context.Background(), c)

	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetArgs(args)
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	err := ExecuteRoot(ctx, root)
	_ = c.Flush(context.Background())
	return err
}

func TestVersion_EmitsCommandInvoked(t *testing.T) {
	mock := telemetrytest.New()
	if err := runCmdWithTelemetry(t, mock, "version"); err != nil {
		t.Fatalf("version: %v", err)
	}
	envs := mock.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envs))
	}
	ci, ok := envs[0].Events[0].(telemetry.CommandInvoked)
	if !ok {
		t.Fatalf("event = %T, want CommandInvoked", envs[0].Events[0])
	}
	props, ok := ci.Properties.(telemetry.VersionCommandInvokedProperties)
	if !ok {
		t.Fatalf("properties = %T, want VersionCommandInvokedProperties", ci.Properties)
	}
	if props.Command != telemetry.CommandNameVersion {
		t.Errorf("Command = %q, want version", props.Command)
	}
	if props.Outcome != telemetry.OutcomeSuccess {
		t.Errorf("Outcome = %q, want success", props.Outcome)
	}
}

func TestRoot_AcceptsHiddenInvocationFlags(t *testing.T) {
	// Cobra must accept --invoker-id / --invoked-by / --invoked-via on
	// every subcommand without an "unknown flag" error. The values
	// themselves are consumed by main.go's argv peek, not from cobra's
	// bound vars, but the flags must be registered or cobra rejects
	// the parse.
	mock := telemetrytest.New()
	err := runCmdWithTelemetry(t, mock,
		"--invoker-id=abc",
		"--invoked-by=vscode",
		"--invoked-via=code_lens",
		"version",
	)
	if err != nil {
		t.Fatalf("version with hidden flags: %v", err)
	}
}

func TestManifest_EmitsCommandInvoked(t *testing.T) {
	mock := telemetrytest.New()
	if err := runCmdWithTelemetry(t, mock, "manifest"); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	envs := mock.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envs))
	}
	props := envs[0].Events[0].(telemetry.CommandInvoked).Properties.(telemetry.ManifestCommandInvokedProperties)
	if props.Command != telemetry.CommandNameManifest {
		t.Errorf("Command = %q, want manifest", props.Command)
	}
	if props.Outcome != telemetry.OutcomeSuccess {
		t.Errorf("Outcome = %q, want success", props.Outcome)
	}
}

func TestManifest_StampsManifestPropsWhenInProject(t *testing.T) {
	dir := setupIntegrationProject(t)
	chdirTo(t, dir)

	mock := telemetrytest.New()
	if err := runCmdWithTelemetry(t, mock, "manifest"); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	envs := mock.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envs))
	}
	props := envs[0].Events[0].(telemetry.CommandInvoked).Properties.(telemetry.ManifestCommandInvokedProperties)
	if len(props.ManifestFeaturesUsed) == 0 {
		t.Error("ManifestFeaturesUsed empty; expected 'projections' / 'fixtures' from setupIntegrationProject")
	}
	if props.ProjectionCount == nil {
		t.Error("ProjectionCount nil; expected non-nil when manifest loaded")
	}
	if props.FixtureCount == nil {
		t.Error("FixtureCount nil; expected non-nil when manifest loaded")
	}
}

func TestManifest_OmitsManifestPropsOutsideProject(t *testing.T) {
	// Switching to a tempdir with no gaffer.toml exercises the
	// best-effort load path: telemetry should still fire, manifest
	// props should be absent.
	chdirTo(t, t.TempDir())

	mock := telemetrytest.New()
	if err := runCmdWithTelemetry(t, mock, "manifest"); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	envs := mock.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envs))
	}
	props := envs[0].Events[0].(telemetry.CommandInvoked).Properties.(telemetry.ManifestCommandInvokedProperties)
	if len(props.ManifestFeaturesUsed) != 0 {
		t.Errorf("ManifestFeaturesUsed = %v, want empty outside project", props.ManifestFeaturesUsed)
	}
	if props.ProjectionCount != nil {
		t.Errorf("ProjectionCount = %v, want nil outside project", *props.ProjectionCount)
	}
	if props.FixtureCount != nil {
		t.Errorf("FixtureCount = %v, want nil outside project", *props.FixtureCount)
	}
}

func TestDev_StampsManifestProps(t *testing.T) {
	dir := setupIntegrationProject(t)
	chdirTo(t, dir)

	mock := telemetrytest.New()
	if err := runCmdWithTelemetry(t, mock, "dev", "orders", "--events", "fixtures/orders.json", "--json"); err != nil {
		t.Fatalf("dev: %v", err)
	}
	envs := mock.Envelopes()
	if len(envs) == 0 {
		t.Fatal("no envelopes")
	}
	// projection_shape may fire before command_invoked; find the
	// command_invoked envelope (exactly one expected).
	var props telemetry.DevCommandInvokedProperties
	var found bool
	for _, env := range envs {
		ci, ok := env.Events[0].(telemetry.CommandInvoked)
		if !ok {
			continue
		}
		p, ok := ci.Properties.(telemetry.DevCommandInvokedProperties)
		if !ok {
			continue
		}
		if found {
			t.Fatal("multiple DevCommandInvokedProperties envelopes; expected exactly one")
		}
		props = p
		found = true
	}
	if !found {
		t.Fatal("no DevCommandInvokedProperties envelope")
	}
	if len(props.ManifestFeaturesUsed) == 0 {
		t.Error("ManifestFeaturesUsed empty; expected at least 'projections' / 'fixtures'")
	}
	if props.ProjectionCount == nil {
		t.Error("ProjectionCount nil; expected non-nil")
	}
	if props.FixtureCount == nil {
		t.Error("FixtureCount nil; expected non-nil")
	}
}

func TestInit_EmitsUserErrorOnRunEFailure(t *testing.T) {
	// `gaffer init` against a directory that already has a
	// gaffer.toml returns "gaffer.toml already exists in <dir>" -
	// the cleanest user_error path through the cmd-level defer +
	// outcomeFor wiring. Covers the failure branch that TestVersion
	// / Manifest's success-only paths don't exercise.
	t.Chdir(t.TempDir())
	if err := os.WriteFile("gaffer.toml", []byte("engine_version = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mock := telemetrytest.New()
	err := runCmdWithTelemetry(t, mock, "init")
	if err == nil {
		t.Fatal("init with existing toml: expected error, got nil")
	}
	envs := mock.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envs))
	}
	props := envs[0].Events[0].(telemetry.CommandInvoked).Properties.(telemetry.InitCommandInvokedProperties)
	if props.Command != telemetry.CommandNameInit {
		t.Errorf("Command = %q, want init", props.Command)
	}
	if props.Outcome != telemetry.OutcomeUserError {
		t.Errorf("Outcome = %q, want user_error", props.Outcome)
	}
}

func TestCmdWithoutClient_DoesNotPanic(t *testing.T) {
	// Sanity check: every one-shot RunE must tolerate a missing
	// Client on ctx (opt-out installs run with no Client at all).
	// We're asserting "no panic", not "no error" - scaffold/info
	// fail their Args validation before RunE, which is fine; what
	// matters is the helper's nil-check holds when RunE does fire.
	//
	// t.Chdir per subtest so `init --yes` (which writes gaffer.toml
	// + .gitignore + .gaffer/) doesn't pollute the package's
	// working directory.
	for _, args := range [][]string{
		{"version"},
		{"manifest"},
		{"init", "--yes"},
		{"scaffold", "Foo"},
		{"info", "Foo"},
	} {
		t.Run(args[0], func(t *testing.T) {
			t.Chdir(t.TempDir())
			root := NewRootCmd()
			var stdout, stderr bytes.Buffer
			root.SetArgs(args)
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			// Errors are expected for commands that need a real
			// project on disk; the surface contract is "doesn't
			// panic," which a successful return enforces.
			_ = ExecuteRoot(context.Background(), root)
		})
	}
}
