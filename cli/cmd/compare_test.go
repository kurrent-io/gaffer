package cmd

import (
	"testing"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

func ledgerEntry(tool, actor string) *remote.Ledger {
	return &remote.Ledger{Tool: tool, Actor: actor, Time: time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)}
}

func TestOwner(t *testing.T) {
	gaffer := ledgerEntry(remote.ToolName, "admin")
	foreign := ledgerEntry("KurrentDB Embedded UI", "jane")
	for _, tc := range []struct {
		name string
		c    comparison
		want ownership
	}{
		{"in-config in-sync", comparison{State: driftInSync}, ownerInConfig},
		{"in-config drifted", comparison{State: driftDrifted}, ownerInConfig},
		{"in-config not-deployed", comparison{State: driftNotDeployed}, ownerInConfig},
		{"in-config invalid", comparison{State: driftInvalid}, ownerInConfig},
		{"untracked, no ledger (degraded/old server)", comparison{State: driftUntracked}, ownerUnknown},
		{"untracked, unreadable ledger", comparison{State: driftUntracked, LedgerErr: remote.ErrMalformedLedger}, ownerUnknown},
		{"untracked, gaffer's entry", comparison{State: driftUntracked, Ledger: gaffer}, ownerOrphan},
		{"untracked, another tool's entry", comparison{State: driftUntracked, Ledger: foreign}, ownerForeign},
	} {
		if got := tc.c.owner(); got != tc.want {
			t.Errorf("%s: owner() = %q, want %q", tc.name, got, tc.want)
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
		c    comparison
		want attribution
	}{
		{"not drifted", comparison{State: driftInSync, Ledger: gaffer, Deployed: deployed, DeployBaseline: same}, attrNone},
		{"drifted, no ledger", comparison{State: driftDrifted, Deployed: deployed, DeployBaseline: same}, attrNone},
		{"drifted, no baseline", comparison{State: driftDrifted, Ledger: gaffer, Deployed: deployed}, attrNone},
		{"deployed matches my gaffer deploy", comparison{State: driftDrifted, Ledger: gaffer, Deployed: deployed, DeployBaseline: same}, attrLocalAhead},
		{"deployed matches another tool's", comparison{State: driftDrifted, Ledger: foreign, Deployed: deployed, DeployBaseline: same}, attrChangedByTool},
		{"deployed differs from the last tool write", comparison{State: driftDrifted, Ledger: gaffer, Deployed: deployed, DeployBaseline: other}, attrChangedServer},
	} {
		if got := tc.c.attribution(); got != tc.want {
			t.Errorf("%s: attribution() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestDriftVerdict(t *testing.T) {
	deployed := desc("q", 2, false)
	for _, tc := range []struct {
		name string
		c    comparison
		want string
	}{
		{"in sync", comparison{State: driftInSync}, "in sync"},
		{"not deployed", comparison{State: driftNotDeployed}, "not deployed"},
		{"orphan", comparison{State: driftUntracked, Ledger: ledgerEntry(remote.ToolName, "")}, "orphan"},
		{"foreign reads as plain untracked", comparison{State: driftUntracked, Ledger: ledgerEntry("KurrentDB Embedded UI", "")}, "untracked"},
		{"unreadable reads as plain untracked", comparison{State: driftUntracked, LedgerErr: remote.ErrMalformedLedger}, "untracked"},
		{"degraded untracked", comparison{State: driftUntracked}, "untracked"},
		{"local ahead", comparison{State: driftDrifted, Ledger: ledgerEntry(remote.ToolName, "a"), Deployed: deployed, DeployBaseline: desc("q", 2, false)}, "local ahead"},
		{"changed externally (server)", comparison{State: driftDrifted, Ledger: ledgerEntry(remote.ToolName, "a"), Deployed: deployed, DeployBaseline: desc("z", 2, false)}, "changed externally"},
		{"changed externally (another tool)", comparison{State: driftDrifted, Ledger: ledgerEntry("KurrentDB Embedded UI", "a"), Deployed: deployed, DeployBaseline: desc("q", 2, false)}, "changed externally"},
		{"drifted, no ledger", comparison{State: driftDrifted}, "drifted"},
	} {
		if got := driftVerdict(tc.c); got != tc.want {
			t.Errorf("%s: driftVerdict() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestLastDeployTime(t *testing.T) {
	ledgerT := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)
	eventT := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC) // distinct from ledgerT
	led := &remote.Ledger{Tool: remote.ToolName, Time: ledgerT}

	// With a ledger, the tool entry's time wins over the deployed event time (the
	// deploy, not a later lifecycle write).
	if got := (comparison{Ledger: led, DeployedAt: eventT}).lastDeployTime(); !got.Equal(ledgerT) {
		t.Errorf("with ledger: lastDeployTime() = %v, want ledger time %v", got, ledgerT)
	}
	// No ledger: falls back to the deployed event time.
	if got := (comparison{DeployedAt: eventT}).lastDeployTime(); !got.Equal(eventT) {
		t.Errorf("no ledger: lastDeployTime() = %v, want event time %v", got, eventT)
	}
	// Neither: zero (not deployed).
	if got := (comparison{}).lastDeployTime(); !got.IsZero() {
		t.Errorf("neither: lastDeployTime() = %v, want zero", got)
	}
}
