package cmd

import "github.com/kurrent-io/gaffer/cli/internal/telemetry"

// outcomeFor maps a RunE return error to the canonical command_invoked
// Outcome. The default mapping is intentionally coarse - nil means
// success, anything else means user_error. Commands that can return
// recognisable error types (manifest not found, projection eval error,
// db disconnect) should map those specifically at the call site
// instead of relying on this fallback.
//
// Internal-error / panic-recover outcomes don't pass through here -
// they're set by the global panic-recover handler in main when it
// stamps the outcome on a recovered envelope.
func outcomeFor(err error) telemetry.Outcome {
	if err == nil {
		return telemetry.OutcomeSuccess
	}
	return telemetry.OutcomeUserError
}
