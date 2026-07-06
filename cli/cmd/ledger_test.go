package cmd

import (
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// An unresolvable env (no --connection, no --env, no default) still stamps
// tool/version/operation - only the actor is dropped. Locks the wrapper's
// resolve-error-to-zero-env mapping, the seam mcpserver bypasses.
func TestToolLedgerUnresolvableEnv(t *testing.T) {
	t.Setenv("GAFFER_ACTOR", "")
	t.Setenv("GAFFER_REVISION", "")
	led := toolLedger("", "", remote.OpDeploy, &config.Config{}, t.TempDir())
	if led.Tool != remote.ToolName || led.Operation != remote.OpDeploy || led.ToolVersion != Version {
		t.Errorf("ledger = %+v, want tool/operation/version stamped", led)
	}
	if led.Actor != "" {
		t.Errorf("actor = %q, want empty for an unresolvable env", led.Actor)
	}
}
