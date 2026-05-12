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
func StartupGate(store *userconfig.Store, cwd, homeDir string, noticeOut io.Writer, opts ...Option) *Client {
	optOut := CheckOptOut(store, cwd, homeDir)
	if optOut.IsDisabled() {
		return nil
	}
	id, err := EnsureIdentity(store, optOut, noticeOut, false)
	if id.IsZero() {
		// Hard failure: mint or persist couldn't produce a usable
		// identity. Leave a one-line trail on noticeOut so users
		// running with telemetry on don't silently get zero events;
		// a partial-load warning paired with a usable id is NOT
		// surfaced here (would spam every CLI run for a recoverable
		// parse problem) - users see those via `config telemetry
		// status` instead.
		if err != nil {
			fmt.Fprintf(noticeOut, "warning: telemetry identity unavailable: %v\n", err)
		}
		return nil
	}
	c := New(opts...)
	c.identity = id
	return c
}
