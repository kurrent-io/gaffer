package drift

import (
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

func TestVerdict(t *testing.T) {
	deployed := desc("q", 2, false)
	for _, tc := range []struct {
		name string
		c    Comparison
		want string
	}{
		{"in sync", Comparison{State: InSync}, "in sync"},
		{"not deployed", Comparison{State: NotDeployed}, "not deployed"},
		{"orphan", Comparison{State: Untracked, Ledger: ledgerEntry(remote.ToolName, "")}, "orphan"},
		{"foreign reads as plain untracked", Comparison{State: Untracked, Ledger: ledgerEntry("KurrentDB Embedded UI", "")}, "untracked"},
		{"unreadable reads as plain untracked", Comparison{State: Untracked, LedgerErr: remote.ErrMalformedLedger}, "untracked"},
		{"degraded untracked", Comparison{State: Untracked}, "untracked"},
		{"local ahead", Comparison{State: Drifted, Ledger: ledgerEntry(remote.ToolName, "a"), Deployed: deployed, DeployBaseline: desc("q", 2, false)}, "local ahead"},
		{"changed externally (server)", Comparison{State: Drifted, Ledger: ledgerEntry(remote.ToolName, "a"), Deployed: deployed, DeployBaseline: desc("z", 2, false)}, "changed externally"},
		{"changed externally (another tool)", Comparison{State: Drifted, Ledger: ledgerEntry("KurrentDB Embedded UI", "a"), Deployed: deployed, DeployBaseline: desc("q", 2, false)}, "changed externally"},
		{"drifted, no ledger", Comparison{State: Drifted}, "drifted"},
	} {
		if got := Verdict(tc.c); got != tc.want {
			t.Errorf("%s: Verdict() = %q, want %q", tc.name, got, tc.want)
		}
	}
}
