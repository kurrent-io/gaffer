package telemetry

import (
	"context"
	"os"
	"runtime"
	"time"
)

// timestampLayout is the wire-format RFC 3339 millisecond layout used
// for every envelope's `timestamp` field. UTC, fixed millisecond
// precision, trailing Z - matches the schema's Timestamp constraint
// regexp at the worker boundary.
const timestampLayout = "2006-01-02T15:04:05.000Z"

// EmitVersion fires command_invoked for `gaffer version`. No-op when
// ctx carries no Client (opt-out or unreadable config).
//
// Callers fill in Outcome (and, in the future, any per-invocation
// overrides for InvokedBy / InvokedVia). Command, DurationMs are
// stamped by the helper from Client state - any value the caller put
// there is discarded.
func EmitVersion(ctx context.Context, p VersionCommandInvokedProperties) {
	c := ClientFromContext(ctx)
	if c == nil {
		return
	}
	c.stampInvocation(&p.Command, &p.DurationMs, &p.InvokedBy, &p.InvokedVia, CommandNameVersion)
	c.fireCommandInvoked(p)
}

// EmitInit fires command_invoked for `gaffer init`.
func EmitInit(ctx context.Context, p InitCommandInvokedProperties) {
	c := ClientFromContext(ctx)
	if c == nil {
		return
	}
	c.stampInvocation(&p.Command, &p.DurationMs, &p.InvokedBy, &p.InvokedVia, CommandNameInit)
	c.fireCommandInvoked(p)
}

// EmitScaffold fires command_invoked for `gaffer scaffold`.
func EmitScaffold(ctx context.Context, p ScaffoldCommandInvokedProperties) {
	c := ClientFromContext(ctx)
	if c == nil {
		return
	}
	c.stampInvocation(&p.Command, &p.DurationMs, &p.InvokedBy, &p.InvokedVia, CommandNameScaffold)
	c.fireCommandInvoked(p)
}

// EmitInfo fires command_invoked for `gaffer info`.
func EmitInfo(ctx context.Context, p InfoCommandInvokedProperties) {
	c := ClientFromContext(ctx)
	if c == nil {
		return
	}
	c.stampInvocation(&p.Command, &p.DurationMs, &p.InvokedBy, &p.InvokedVia, CommandNameInfo)
	c.fireCommandInvoked(p)
}

// EmitManifest fires command_invoked for `gaffer manifest`. Optional
// per-variant fields (ManifestFeaturesUsed, ProjectionCount,
// FixtureCount) stay absent on the wire when the caller leaves them
// zero.
func EmitManifest(ctx context.Context, p ManifestCommandInvokedProperties) {
	c := ClientFromContext(ctx)
	if c == nil {
		return
	}
	c.stampInvocation(&p.Command, &p.DurationMs, &p.InvokedBy, &p.InvokedVia, CommandNameManifest)
	c.fireCommandInvoked(p)
}

// stampInvocation fills in the four base fields that the emit-side
// owns: Command (literal per helper), DurationMs (time since Client
// start), InvokedBy / InvokedVia (defaults if the caller didn't set
// them via flags / overrides). The pointer-args shape lets us mutate
// the variant struct in place without per-variant type plumbing
// while keeping the call sites identical across all five EmitX
// helpers - a property the generator can rely on.
func (c *Client) stampInvocation(command *CommandName, duration *RawCount, invokedBy *InvokedBy, invokedVia *InvokedVia, name CommandName) {
	*command = name
	*duration = RawCount(time.Since(c.startTime).Milliseconds())
	if *invokedBy == "" {
		*invokedBy = InvokedByDirect
	}
	if *invokedVia == "" {
		*invokedVia = InvokedViaTerminal
	}
}

// fireCommandInvoked wraps variant properties in a CommandInvoked
// event with the current timestamp and hands the envelope to the
// Client's async sink. The `any` parameter is the single, contained
// erasure point - the gen'd `CommandInvoked.Properties any` field
// forces it, and each EmitX guarantees a concrete variant type at
// the call site.
func (c *Client) fireCommandInvoked(props any) {
	c.emit(c.buildEnvelope(CommandInvoked{
		Name:       "command_invoked",
		Timestamp:  nowTimestamp(),
		Properties: props,
	}))
}

// buildEnvelope wraps a single event in the standard envelope shape.
// Reads identity (TelemetryID -> emitter_id, RunID -> run_id) and
// libVersion off the Client; reads OS / arch from runtime; detects
// runtime_environment from CI env vars.
func (c *Client) buildEnvelope(ev Event) *Envelope {
	return &Envelope{
		SchemaVersion: SchemaVersion,
		EmitterID:     c.identity.TelemetryID,
		RunID:         c.identity.RunID,
		Context: Context{
			Emitter:            EmitterCLI,
			LibVersion:         c.libVersion,
			OS:                 mapGoOS(runtime.GOOS),
			Arch:               mapGoArch(runtime.GOARCH),
			RuntimeEnvironment: detectRuntimeEnv(),
		},
		Events: []Event{ev},
	}
}

// mapGoOS translates a runtime.GOOS string to the schema's OS enum.
// For darwin / linux / windows the literals line up. Anything else is
// passed through unchanged - the worker drops envelopes with values
// outside the enum, so unrecognised OSes don't get misattributed.
func mapGoOS(goos string) OS {
	switch goos {
	case "darwin":
		return OSDarwin
	case "linux":
		return OSLinux
	case "windows":
		return OSWindows
	}
	return OS(goos)
}

// mapGoArch translates a runtime.GOARCH string to the schema's Arch
// enum. The Go names ("amd64", "386", "arm64") don't match the
// schema's ("x64", "ia32", "arm64") for two of three, hence the
// explicit table.
func mapGoArch(goarch string) Arch {
	switch goarch {
	case "amd64":
		return ArchX64
	case "arm64":
		return ArchArm64
	case "386":
		return ArchIA32
	}
	return Arch(goarch)
}

// detectRuntimeEnv flips to RuntimeEnvironmentCI when any common CI
// provider env var is set. GitHub Actions / GitLab / CircleCI /
// Travis / Buildkite / AppVeyor / Drone all set CI=true. TeamCity
// sets TEAMCITY_VERSION but not CI. Jenkins sets JENKINS_URL but
// only sets CI via the default-on CI plugin, so check both.
// Misclassification only leaks a "local" tag on a CI run, which is
// recoverable downstream.
func detectRuntimeEnv() RuntimeEnvironment {
	for _, k := range []string{"CI", "TEAMCITY_VERSION", "JENKINS_URL"} {
		if os.Getenv(k) != "" {
			return RuntimeEnvironmentCI
		}
	}
	return RuntimeEnvironmentLocal
}

// nowTimestamp returns the current wall-clock time formatted to the
// schema's Timestamp shape. UTC so envelopes from disparate machines
// sort consistently; millisecond precision matches the worker's
// expectation.
func nowTimestamp() Timestamp {
	return time.Now().UTC().Format(timestampLayout)
}
