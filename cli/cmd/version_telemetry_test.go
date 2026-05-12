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
// StartupGate, so they need WithIdentity (re-exported in commit 8 for
// exactly this case) to stamp envelopes without minting.
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

func TestInit_EmitsUserErrorOnRunEFailure(t *testing.T) {
	// `gaffer init` without --yes returns "interactive mode not
	// yet supported, use --yes / -y" - the cleanest user_error
	// path through the cmd-level defer + outcomeFor wiring. Covers
	// the failure branch that TestVersion/Manifest's success-only
	// paths don't exercise.
	mock := telemetrytest.New()
	err := runCmdWithTelemetry(t, mock, "init")
	if err == nil {
		t.Fatal("init without --yes: expected error, got nil")
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
