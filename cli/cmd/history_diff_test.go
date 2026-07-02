package cmd

import (
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

func TestHistoryDiffAt(t *testing.T) {
	hist := classifyHistory([]remote.Version{
		ver(4, "fromAll()\ncount(3)\n", true, gafferLedger(remote.OpDeploy)), // 0: deploy
		ver(3, "fromAll()\ncount(2)\n", false, nil),                          // 1: disabled (same content as 2)
		ver(2, "fromAll()\ncount(2)\n", true, gafferLedger(remote.OpDeploy)), // 2: deploy
		ver(1, "fromAll()\ncount(1)\n", true, nil),                           // 3: edited externally
		ver(0, "fromAll()\ncount(1)\n", true, nil),                           // 4: created
	})

	t.Run("content vs previous content, skipping the state change", func(t *testing.T) {
		d := historyDiffAt(hist, 0, false)
		if d.state != diffReady || d.base == nil {
			t.Fatalf("state=%v base=%v, want ready with a baseline", d.state, d.base)
		}
		if d.base.Hash != hist[2].Hash {
			t.Errorf("baseline = %q (kind %v), want the deploy at index 2 - the disabled row must be skipped", d.base.Hash, d.base.Kind)
		}
		var added, removed int
		for _, l := range d.lines {
			switch l.Kind {
			case deploy.LineAdded:
				added++
			case deploy.LineRemoved:
				removed++
			}
		}
		if added != 1 || removed != 1 {
			t.Errorf("diff = +%d -%d, want the one-line count change", added, removed)
		}
	})

	t.Run("state change shows no definition change", func(t *testing.T) {
		if d := historyDiffAt(hist, 1, false); d.state != diffNoChange {
			t.Errorf("state = %v, want diffNoChange for a disabled row", d.state)
		}
	})

	t.Run("first version diffs from empty", func(t *testing.T) {
		d := historyDiffAt(hist, 4, false)
		if d.state != diffReady || d.base != nil {
			t.Fatalf("state=%v base=%v, want ready from empty", d.state, d.base)
		}
		for _, l := range d.lines {
			if l.Kind != deploy.LineAdded {
				t.Errorf("line %q is %v, want all-added from empty", l.Text, l.Kind)
			}
		}
	})

	t.Run("unloaded baseline is unknown, not created", func(t *testing.T) {
		if d := historyDiffAt(hist, 4, true); d.state != diffBaselineUnloaded {
			t.Errorf("state = %v, want diffBaselineUnloaded when older pages exist", d.state)
		}
	})

	t.Run("scalar changes ride the comparison", func(t *testing.T) {
		h := classifyHistory([]remote.Version{
			{Number: 1, Definition: &remote.Definition{Query: "q\n", EngineVersion: 2, Enabled: true, Time: histTime}},
			{Number: 0, Definition: &remote.Definition{Query: "q\n", EngineVersion: 1, Enabled: true, Time: histTime}, Ledger: gafferLedger(remote.OpDeploy)},
		})
		d := historyDiffAt(h, 0, false)
		if d.state != diffReady || !d.cmp.EngineVersionDiffers {
			t.Errorf("state=%v cmp=%+v, want ready with engine version flagged", d.state, d.cmp)
		}
	})
}
