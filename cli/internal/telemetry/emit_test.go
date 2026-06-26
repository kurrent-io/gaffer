package telemetry

import (
	"context"
	"os"
	"regexp"
	"runtime"
	"testing"
	"time"
)

// testIdentity is a fixed in-package value so emit tests can assert on
// the envelope's emitter_id / run_id without going through MintIdentity.
var testIdentity = Identity{
	TelemetryID: "11111111-1111-1111-1111-111111111111",
	Salt:        "22222222-2222-2222-2222-222222222222",
	RunID:       "33333333-3333-3333-3333-333333333333",
}

// timestampPattern asserts the RFC 3339 millisecond shape without
// pinning the value.
var timestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)

// emitTestSetup builds a Client wired to a MockSink with a fixed
// identity and returns a ctx carrying the Client + the sink for
// inspection. Use in every emit_test case so the surface stays
// consistent.
func emitTestSetup(t *testing.T) (context.Context, *Client, *internalMockSink) {
	t.Helper()
	mock := newMockSink()
	c := New(
		WithSink(mock),
		WithIdentity(testIdentity),
		WithLibVersion("0.0.1-test"),
	)
	ctx := WithClient(context.Background(), c)
	t.Cleanup(func() {
		_ = c.Flush(timeoutCtx(t, time.Second))
	})
	return ctx, c, mock
}

func TestEmit_NilClientIsNoop(t *testing.T) {
	// Empty ctx -> ClientFromContext returns nil -> emit silently
	// drops. Exercising each helper guards against a future helper
	// being added that forgets the nil-check.
	for _, fn := range []func(){
		func() { EmitVersion(context.Background(), VersionCommandInvokedProperties{Outcome: OutcomeSuccess}) },
		func() { EmitInit(context.Background(), InitCommandInvokedProperties{Outcome: OutcomeSuccess}) },
		func() { EmitScaffold(context.Background(), ScaffoldCommandInvokedProperties{Outcome: OutcomeSuccess}) },
		func() { EmitInfo(context.Background(), InfoCommandInvokedProperties{Outcome: OutcomeSuccess}) },
		func() { EmitManifest(context.Background(), ManifestCommandInvokedProperties{Outcome: OutcomeSuccess}) },
	} {
		fn() // must not panic
	}
}

func TestEmitVersion_BuildsEnvelope(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)

	EmitVersion(ctx, VersionCommandInvokedProperties{Outcome: OutcomeSuccess})
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}

	envs := mock.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envs))
	}
	env := envs[0]
	if env.SchemaVersion != EnvelopeSchemaVersion1 {
		t.Errorf("SchemaVersion = %q, want %q", env.SchemaVersion, EnvelopeSchemaVersion1)
	}
	if env.EmitterID != testIdentity.TelemetryID {
		t.Errorf("EmitterID = %q, want %q", env.EmitterID, testIdentity.TelemetryID)
	}
	if env.RunID != testIdentity.RunID {
		t.Errorf("RunID = %q, want %q", env.RunID, testIdentity.RunID)
	}
	if env.Context.Emitter != EmitterCLI {
		t.Errorf("Emitter = %q, want %q", env.Context.Emitter, EmitterCLI)
	}
	if env.Context.LibVersion != "0.0.1-test" {
		t.Errorf("LibVersion = %q, want %q", env.Context.LibVersion, "0.0.1-test")
	}
	if len(env.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(env.Events))
	}
	ci, ok := env.Events[0].(CommandInvoked)
	if !ok {
		t.Fatalf("event type = %T, want CommandInvoked", env.Events[0])
	}
	if ci.Name != "command_invoked" {
		t.Errorf("event name = %q, want command_invoked", ci.Name)
	}
	if !timestampPattern.MatchString(ci.Timestamp) {
		t.Errorf("timestamp %q doesn't match RFC 3339 ms pattern", ci.Timestamp)
	}
	props, ok := ci.Properties.(VersionCommandInvokedProperties)
	if !ok {
		t.Fatalf("properties = %T, want VersionCommandInvokedProperties", ci.Properties)
	}
	if props.Command != CommandNameVersion {
		t.Errorf("Command = %q, want %q", props.Command, CommandNameVersion)
	}
	if props.Outcome != OutcomeSuccess {
		t.Errorf("Outcome = %q, want success", props.Outcome)
	}
	if props.InvokedBy != InvokedByDirect {
		t.Errorf("InvokedBy = %q, want direct", props.InvokedBy)
	}
	if props.InvokedVia != nil {
		t.Errorf("InvokedVia = %v, want nil (no flag, no default)", *props.InvokedVia)
	}
	if props.DurationMs < 0 {
		t.Errorf("DurationMs = %d, want non-negative", props.DurationMs)
	}
}

func TestBuildEnvelope_EmitterIsMCPWhenMCPInFlight(t *testing.T) {
	// wire.cue requires emitter=mcp when gaffer's own MCP server
	// produced the envelope. emitterFor reads from currentCommand
	// so any envelope (command_invoked, projection_shape, exception)
	// emitted during an MCP session gets the right surface tag.
	ctx, c, mock := emitTestSetup(t)
	_ = ctx
	c.setCurrentCommand(CommandNameMCP)
	c.emit(c.buildEnvelope(CommandInvoked{Name: "command_invoked", Timestamp: nowTimestamp()}))
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	envs := mock.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envs))
	}
	if envs[0].Context.Emitter != EmitterMCP {
		t.Errorf("Emitter = %q, want mcp", envs[0].Context.Emitter)
	}
}

func TestBuildEnvelope_EmitterDefaultsToCLI(t *testing.T) {
	// No Begin/Emit fired yet (or one of the non-mcp commands):
	// emitter falls back to cli.
	ctx, c, mock := emitTestSetup(t)
	_ = ctx
	c.setCurrentCommand(CommandNameDev)
	c.emit(c.buildEnvelope(CommandInvoked{Name: "command_invoked", Timestamp: nowTimestamp()}))
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if envs := mock.Envelopes(); envs[0].Context.Emitter != EmitterCLI {
		t.Errorf("Emitter = %q, want cli", envs[0].Context.Emitter)
	}
}

func TestEmit_VariantSelectsCorrectPropertiesType(t *testing.T) {
	cases := []struct {
		name string
		emit func(context.Context)
		want CommandName
	}{
		{"init", func(c context.Context) {
			EmitInit(c, InitCommandInvokedProperties{Outcome: OutcomeSuccess})
		}, CommandNameInit},
		{"scaffold", func(c context.Context) {
			EmitScaffold(c, ScaffoldCommandInvokedProperties{Outcome: OutcomeSuccess})
		}, CommandNameScaffold},
		{"info", func(c context.Context) {
			EmitInfo(c, InfoCommandInvokedProperties{Outcome: OutcomeSuccess})
		}, CommandNameInfo},
		{"manifest", func(c context.Context) {
			EmitManifest(c, ManifestCommandInvokedProperties{Outcome: OutcomeSuccess})
		}, CommandNameManifest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, c, mock := emitTestSetup(t)
			tc.emit(ctx)
			if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
				t.Fatalf("flush: %v", err)
			}
			envs := mock.Envelopes()
			if len(envs) != 1 {
				t.Fatalf("envelopes = %d, want 1", len(envs))
			}
			ci := envs[0].Events[0].(CommandInvoked)
			// Discriminate on the embedded base.Command rather
			// than the variant type so the test stays open to
			// per-variant fields without restructuring.
			var got CommandName
			switch p := ci.Properties.(type) {
			case InitCommandInvokedProperties:
				got = p.Command
			case ScaffoldCommandInvokedProperties:
				got = p.Command
			case InfoCommandInvokedProperties:
				got = p.Command
			case ManifestCommandInvokedProperties:
				got = p.Command
			default:
				t.Fatalf("unexpected properties type %T", ci.Properties)
			}
			if got != tc.want {
				t.Errorf("Command = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEmit_OutcomePropagates(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	EmitInit(ctx, InitCommandInvokedProperties{Outcome: OutcomeUserError})
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	envs := mock.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envs))
	}
	props := envs[0].Events[0].(CommandInvoked).Properties.(InitCommandInvokedProperties)
	if props.Outcome != OutcomeUserError {
		t.Errorf("Outcome = %q, want user_error", props.Outcome)
	}
}

func TestEmitManifest_PopulatesOptionalFields(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	projections := RawCount(3)
	fixtures := RawCount(12)
	EmitManifest(ctx, ManifestCommandInvokedProperties{
		Outcome:              OutcomeSuccess,
		ManifestFeaturesUsed: []string{"projections", "fixtures"},
		ProjectionCount:      &projections,
		FixtureCount:         &fixtures,
	})
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	props := mock.Envelopes()[0].Events[0].(CommandInvoked).Properties.(ManifestCommandInvokedProperties)
	if len(props.ManifestFeaturesUsed) != 2 {
		t.Errorf("FeaturesUsed = %v, want 2 items", props.ManifestFeaturesUsed)
	}
	if props.ProjectionCount == nil || *props.ProjectionCount != 3 {
		t.Errorf("ProjectionCount = %v, want &3", props.ProjectionCount)
	}
	if props.FixtureCount == nil || *props.FixtureCount != 12 {
		t.Errorf("FixtureCount = %v, want &12", props.FixtureCount)
	}
}

func TestEmitManifest_OptionalFieldsAbsentByDefault(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	EmitManifest(ctx, ManifestCommandInvokedProperties{Outcome: OutcomeSuccess})
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	props := mock.Envelopes()[0].Events[0].(CommandInvoked).Properties.(ManifestCommandInvokedProperties)
	if props.ManifestFeaturesUsed != nil {
		t.Errorf("FeaturesUsed = %v, want nil", props.ManifestFeaturesUsed)
	}
	if props.ProjectionCount != nil {
		t.Errorf("ProjectionCount = %v, want nil", props.ProjectionCount)
	}
	if props.FixtureCount != nil {
		t.Errorf("FixtureCount = %v, want nil", props.FixtureCount)
	}
}

func TestMapGoArch(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Arch
	}{
		{"amd64", ArchX64},
		{"arm64", ArchArm64},
		{"386", ArchIA32},
	} {
		if got := mapGoArch(tc.in); got != tc.want {
			t.Errorf("mapGoArch(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// Unknown goarch is passed through unchanged so the worker can
	// drop it rather than us misattributing.
	if got := mapGoArch("mips"); got != Arch("mips") {
		t.Errorf("mapGoArch(mips) = %q, want passthrough", got)
	}
}

func TestMapGoOS(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want OS
	}{
		{"darwin", OSDarwin},
		{"linux", OSLinux},
		{"windows", OSWindows},
	} {
		if got := mapGoOS(tc.in); got != tc.want {
			t.Errorf("mapGoOS(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDetectRuntimeEnv(t *testing.T) {
	// t.Setenv-empty then Unsetenv: t.Setenv arranges restoration
	// at cleanup; Unsetenv inside the test makes os.Getenv return
	// "" so the local branch fires regardless of the runner's
	// inherited CI=true.
	t.Setenv("CI", "")
	_ = os.Unsetenv("CI")
	if got := detectRuntimeEnv(); got != RuntimeEnvironmentLocal {
		t.Errorf("no CI env = %q, want local", got)
	}

	t.Setenv("CI", "true")
	if got := detectRuntimeEnv(); got != RuntimeEnvironmentCI {
		t.Errorf("CI=true = %q, want ci", got)
	}
}

func TestEnvelope_OSArchReflectRuntime(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	EmitVersion(ctx, VersionCommandInvokedProperties{Outcome: OutcomeSuccess})
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	env := mock.Envelopes()[0]
	if env.Context.OS != mapGoOS(runtime.GOOS) {
		t.Errorf("Context.OS = %q, want %q", env.Context.OS, mapGoOS(runtime.GOOS))
	}
	if env.Context.Arch != mapGoArch(runtime.GOARCH) {
		t.Errorf("Context.Arch = %q, want %q", env.Context.Arch, mapGoArch(runtime.GOARCH))
	}
}
