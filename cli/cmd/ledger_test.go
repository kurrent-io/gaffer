package cmd

import (
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// A zero resolved env (an ad-hoc --connection has no env name or overlay)
// still stamps tool/version/operation - only the actor is dropped.
func TestToolLedgerZeroEnv(t *testing.T) {
	t.Setenv("GAFFER_ACTOR", "")
	t.Setenv("GAFFER_REVISION", "")
	led := toolLedger(config.ResolvedEnv{}, remote.OpDeploy, t.TempDir())
	if led.Tool != remote.ToolName || led.Operation != remote.OpDeploy || led.ToolVersion != Version {
		t.Errorf("ledger = %+v, want tool/operation/version stamped", led)
	}
	if led.Actor != "" {
		t.Errorf("actor = %q, want empty for a zero env", led.Actor)
	}
}
