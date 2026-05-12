package telemetry

import (
	"fmt"
	"io"

	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

// StartupGate is the canonical main-side composer for "should we
// build a telemetry Client for this process?". It:
//
//  1. checks the three-layer opt-out cascade (user, env, workspace)
//  2. if not opted out, resolves identity (loading the persisted
//     id/salt pair, or minting + persisting a fresh one and
//     printing the disclosure notice to noticeOut on first run)
//  3. constructs a Client stamped with the resolved identity plus
//     any extra options the caller supplies (typically WithUserAgent)
//
// Returns nil Client when:
//   - any opt-out layer is active (the user said no, or env said
//     no, or the workspace gaffer.toml said no);
//   - identity resolution returned a zero Identity (mint failure
//     or race-recovery couldn't adopt a winner).
//
// A nil return is the "telemetry off for this run" signal. main.go
// passes it to telemetry.WithClient anyway; ClientFromContext +
// Client.Identity both nil-tolerate and emit helpers fall through
// to no-op. Caller does NOT need to inspect the return value.
//
// noticeOut is typically os.Stderr - main has clean access to it
// before any subcommand has written to stdout/stderr, which is the
// right moment for first-mint disclosure. Routing the notice through
// a subcommand's writer would risk it landing mid-TUI for long-
// running surfaces (`gaffer dev`, `gaffer lsp`).
//
// cwd and homeDir are passed in (not read via os.Getwd / os.UserHomeDir
// inside) so tests can drive the function deterministically. main.go
// reads them once at startup and forwards.
//
// projectRoot is the absolute path to the gaffer project root for
// this process (empty when launched outside a project). main.go
// resolves it via project.FindRootFrom(cwd) and passes it in so
// telemetry stays free of project-discovery imports - this layer
// only knows how to hash. The result is stamped onto every envelope
// via Context.ProjectID.
//
// Long-running surfaces (lsp, mcp) currently inherit the launch-cwd's
// project_id for every event they emit, even if they serve requests
// from other workspace roots. Acceptable today because those events
// are command_invoked only; revisit when LSP/MCP gain per-request
// telemetry.
//
// invocation carries the spawn-linkage values parsed from the hidden
// root flags. When InvokerID is set, the first-mint disclosure notice
// is suppressed (the spawning surface, typically the VS Code
// extension's first-activation flow, has already disclosed and
// printing to a non-TTY stderr inside an extension-spawned CLI would
// be invisible anyway). The full Invocation is also stamped onto the
// Client so emit-side defaults can read it.
func StartupGate(store *userconfig.Store, cwd, homeDir, projectRoot string, noticeOut io.Writer, invocation Invocation, opts ...Option) *Client {
	optOut := CheckOptOut(store, cwd, homeDir)
	if optOut.IsDisabled() {
		return nil
	}
	id, err := EnsureIdentity(store, optOut, noticeOut, !invocation.IsZero())
	if id.IsZero() {
		// Hard failure: mint or persist couldn't produce a usable
		// identity. Leave a one-line trail on noticeOut so users
		// running with telemetry on don't silently get zero events;
		// a partial-load warning paired with a usable id is NOT
		// surfaced here (would spam every CLI run for a recoverable
		// parse problem) - users see those via `config telemetry
		// status` instead.
		if err != nil {
			_, _ = fmt.Fprintf(noticeOut, "warning: telemetry identity unavailable: %v\n", err)
		}
		return nil
	}
	// Hash the project root if main.go resolved one. Schema makes
	// project_id optional; "absent" is the not-in-a-project signal.
	var projectID string
	if projectRoot != "" {
		projectID = ProjectID(id.Salt, projectRoot)
	}
	return New(append(opts,
		WithIdentity(id),
		WithInvocation(invocation),
		WithProjectID(projectID),
	)...)
}
