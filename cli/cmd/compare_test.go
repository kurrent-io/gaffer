package cmd

import (
	"testing"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

func ledgerEntry(tool, actor string) *remote.Ledger {
	return &remote.Ledger{Tool: tool, Actor: actor, Time: time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)}
}

func TestDriftVerdict(t *testing.T) {
	deployed := desc("q", 2, false)
	for _, tc := range []struct {
		name string
		c    drift.Comparison
		want string
	}{
		{"in sync", drift.Comparison{State: drift.InSync}, "in sync"},
		{"not deployed", drift.Comparison{State: drift.NotDeployed}, "not deployed"},
		{"orphan", drift.Comparison{State: drift.Untracked, Ledger: ledgerEntry(remote.ToolName, "")}, "orphan"},
		{"foreign reads as plain untracked", drift.Comparison{State: drift.Untracked, Ledger: ledgerEntry("KurrentDB Embedded UI", "")}, "untracked"},
		{"unreadable reads as plain untracked", drift.Comparison{State: drift.Untracked, LedgerErr: remote.ErrMalformedLedger}, "untracked"},
		{"degraded untracked", drift.Comparison{State: drift.Untracked}, "untracked"},
		{"local ahead", drift.Comparison{State: drift.Drifted, Ledger: ledgerEntry(remote.ToolName, "a"), Deployed: deployed, DeployBaseline: desc("q", 2, false)}, "local ahead"},
		{"changed externally (server)", drift.Comparison{State: drift.Drifted, Ledger: ledgerEntry(remote.ToolName, "a"), Deployed: deployed, DeployBaseline: desc("z", 2, false)}, "changed externally"},
		{"changed externally (another tool)", drift.Comparison{State: drift.Drifted, Ledger: ledgerEntry("KurrentDB Embedded UI", "a"), Deployed: deployed, DeployBaseline: desc("q", 2, false)}, "changed externally"},
		{"drifted, no ledger", drift.Comparison{State: drift.Drifted}, "drifted"},
	} {
		if got := driftVerdict(tc.c); got != tc.want {
			t.Errorf("%s: driftVerdict() = %q, want %q", tc.name, got, tc.want)
		}
	}
}
