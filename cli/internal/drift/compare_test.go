package drift

import (
	"testing"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

func ledgerEntry(tool, actor string) *remote.Ledger {
	return &remote.Ledger{Tool: tool, Actor: actor, Time: time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)}
}

func desc(query string, engineVersion int, emit bool) *deploy.Descriptor {
	return &deploy.Descriptor{Query: query, EngineVersion: engineVersion, Emit: emit}
}

func TestOwner(t *testing.T) {
	gaffer := ledgerEntry(remote.ToolName, "admin")
	foreign := ledgerEntry("KurrentDB Embedded UI", "jane")
	for _, tc := range []struct {
		name string
		c    Comparison
		want Ownership
	}{
		{"in-config in-sync", Comparison{State: InSync}, OwnerInConfig},
		{"in-config drifted", Comparison{State: Drifted}, OwnerInConfig},
		{"in-config not-deployed", Comparison{State: NotDeployed}, OwnerInConfig},
		{"in-config invalid", Comparison{State: Invalid}, OwnerInConfig},
		{"untracked, no ledger (degraded/old server)", Comparison{State: Untracked}, OwnerUnknown},
		{"untracked, unreadable ledger", Comparison{State: Untracked, LedgerErr: remote.ErrMalformedLedger}, OwnerUnknown},
		{"untracked, gaffer's entry", Comparison{State: Untracked, Ledger: gaffer}, OwnerOrphan},
		{"untracked, another tool's entry", Comparison{State: Untracked, Ledger: foreign}, OwnerForeign},
	} {
		if got := tc.c.Owner(); got != tc.want {
			t.Errorf("%s: Owner() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestAttribution(t *testing.T) {
	gaffer := ledgerEntry(remote.ToolName, "admin")
	foreign := ledgerEntry("KurrentDB Embedded UI", "jane")
	deployed := desc("query-a", 2, false)
	same := desc("query-a", 2, false)  // same content hash as deployed
	other := desc("query-b", 2, false) // different content hash
	for _, tc := range []struct {
		name string
		c    Comparison
		want Attribution
	}{
		{"not drifted", Comparison{State: InSync, Ledger: gaffer, Deployed: deployed, DeployBaseline: same}, AttrNone},
		{"drifted, no ledger", Comparison{State: Drifted, Deployed: deployed, DeployBaseline: same}, AttrNone},
		{"drifted, no baseline", Comparison{State: Drifted, Ledger: gaffer, Deployed: deployed}, AttrNone},
		{"deployed matches my gaffer deploy", Comparison{State: Drifted, Ledger: gaffer, Deployed: deployed, DeployBaseline: same}, AttrLocalAhead},
		{"deployed matches another tool's", Comparison{State: Drifted, Ledger: foreign, Deployed: deployed, DeployBaseline: same}, AttrChangedByTool},
		{"deployed differs from the last tool write", Comparison{State: Drifted, Ledger: gaffer, Deployed: deployed, DeployBaseline: other}, AttrChangedServer},
	} {
		if got := tc.c.Attribution(); got != tc.want {
			t.Errorf("%s: Attribution() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestLastDeployTime(t *testing.T) {
	ledgerT := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)
	eventT := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC) // distinct from ledgerT
	led := &remote.Ledger{Tool: remote.ToolName, Time: ledgerT}

	// With a ledger, the tool entry's time wins over the deployed event time (the
	// deploy, not a later lifecycle write).
	if got := (Comparison{Ledger: led, DeployedAt: eventT}).LastDeployTime(); !got.Equal(ledgerT) {
		t.Errorf("with ledger: LastDeployTime() = %v, want ledger time %v", got, ledgerT)
	}
	// No ledger: falls back to the deployed event time.
	if got := (Comparison{DeployedAt: eventT}).LastDeployTime(); !got.Equal(eventT) {
		t.Errorf("no ledger: LastDeployTime() = %v, want event time %v", got, eventT)
	}
	// Neither: zero (not deployed).
	if got := (Comparison{}).LastDeployTime(); !got.IsZero() {
		t.Errorf("neither: LastDeployTime() = %v, want zero", got)
	}
}
