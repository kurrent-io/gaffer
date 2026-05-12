package telemetry

import (
	"os"
	"runtime"
	"time"
)

// Hand-written runtime that the generated emit.gen.go binds against.
// EmitX helpers (one per one-shot command_invoked variant) are
// generated; the Client-side shared pieces - envelope construction,
// base-field stamping, OS/arch mapping, CI detection, timestamp
// format - live here.

// timestampLayout is the wire-format RFC 3339 millisecond layout used
// for every envelope's `timestamp` field. UTC, fixed millisecond
// precision, trailing Z - matches the schema's Timestamp constraint
// regexp at the worker boundary.
const timestampLayout = "2006-01-02T15:04:05.000Z"

// stampInvocation fills in the four emit-side base fields on a
// one-shot variant's properties: Command (literal per helper),
// DurationMs (time since Client start), InvokedBy / InvokedVia
// (defaults if the caller didn't set them via flags / overrides).
// The pointer-arg shape lets one body handle all five one-shot
// variants without per-variant type plumbing; generated EmitX
// helpers pass pointers into their own props fields.
func (c *Client) stampInvocation(command *CommandName, duration *RawCount, invokedBy *InvokedBy, invokedVia *InvokedVia, name CommandName) {
	*command = name
	*duration = RawCount(time.Since(c.startTime).Milliseconds())
	if *invokedBy == "" {
		*invokedBy = c.defaultInvokedBy(name)
	}
	if *invokedVia == "" {
		*invokedVia = c.defaultInvokedVia(name)
	}
}

// fireCommandInvoked wraps variant properties in a CommandInvoked
// event with the current timestamp and hands the envelope to the
// Client's async sink. The `any` parameter is the single, contained
// erasure point - the gen'd `CommandInvoked.Properties any` field
// forces it, and each generated EmitX / End guarantees a concrete
// variant type at the call site.
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
	ctx := Context{
		Emitter:            EmitterCLI,
		LibVersion:         c.libVersion,
		OS:                 mapGoOS(runtime.GOOS),
		Arch:               mapGoArch(runtime.GOARCH),
		RuntimeEnvironment: detectRuntimeEnv(),
	}
	if c.invocation.InvokerID != "" {
		id := c.invocation.InvokerID
		ctx.InvokerID = &id
	}
	return &Envelope{
		SchemaVersion: SchemaVersion,
		EmitterID:     c.identity.TelemetryID,
		RunID:         c.identity.RunID,
		Context:       ctx,
		Events:        []Event{ev},
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
