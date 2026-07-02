package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

var histTime = time.Date(2026, 6, 28, 14, 32, 0, 0, time.UTC)

func ver(number int64, query string, enabled bool, l *remote.Ledger) remote.Version {
	return remote.Version{
		Number:     number,
		Definition: &remote.Definition{Query: query, EngineVersion: 1, Enabled: enabled, Time: histTime},
		Ledger:     l,
	}
}

func gafferLedger(op string) *remote.Ledger {
	return &remote.Ledger{Tool: remote.ToolName, Operation: op, ToolVersion: "1.4.0", Actor: "george@kurrent.io", Revision: "9f8e7d6", Time: histTime}
}

func TestClassifyVersion(t *testing.T) {
	prev := ver(0, "v0", true, nil)
	disabledPrev := ver(0, "v0", false, nil)
	enabledPrev := ver(0, "v0", true, nil)
	for _, tc := range []struct {
		name     string
		v        remote.Version
		prev     *remote.Version
		wantKind versionKind
		wantTool string
		wantExt  bool
	}{
		{"gaffer deploy", ver(1, "v1", true, gafferLedger(remote.OpDeploy)), &prev, kindDeploy, "", false},
		{"gaffer rollback", ver(1, "v1", true, gafferLedger(remote.OpRollback)), &prev, kindRollback, "", false},
		{"gaffer reset", ver(1, "v1", true, gafferLedger(remote.OpReset)), &prev, kindReset, "", false},
		{"gaffer recreate", ver(1, "v1", true, gafferLedger(remote.OpRecreate)), &prev, kindRecreate, "", false},
		{"foreign tool", ver(1, "v1", true, &remote.Ledger{Tool: "KurrentDB Embedded UI", Operation: "create", Time: histTime}), &prev, kindChangedByTool, "KurrentDB Embedded UI", true},
		{"metadata-less query change", ver(1, "changed", true, nil), &prev, kindEditedExternally, "", true},
		{"metadata-less enable", ver(1, "v0", true, nil), &disabledPrev, kindEnabled, "", false},
		{"metadata-less disable", ver(1, "v0", false, nil), &enabledPrev, kindDisabled, "", false},
		{"absent enabled is disabled (flip from enabled)", remote.Version{Number: 1, Definition: &remote.Definition{Query: "v0", EngineVersion: 1, Time: histTime}}, &enabledPrev, kindDisabled, "", false},
		{"config change (reconfigured)", remote.Version{Number: 1, Definition: &remote.Definition{Query: "v0", EngineVersion: 1, Enabled: true, Config: remote.Config{MaxWriteBatchLength: 1000}, Time: histTime}}, &prev, kindReconfigured, "", false},
		{"metadata-less no-op", ver(1, "v0", true, nil), &prev, kindRewritten, "", false},
		{"metadata-less first version", ver(0, "v0", true, nil), nil, kindCreated, "", false},
		{"metadata-less oldest in window", ver(5, "v5", true, nil), nil, kindRewritten, "", false},
		{"tombstone", remote.Version{Number: 2, Deleted: true, Definition: &remote.Definition{Query: "v2", Time: histTime}, Ledger: gafferLedger(remote.OpDeploy)}, &prev, kindDeleted, "", false},
		{"unreadable metadata", remote.Version{Number: 1, Definition: &remote.Definition{Query: "v1", Time: histTime}, MetaErr: errors.New("bad metadata")}, &prev, kindUnreadable, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			kind, tool, _, _ := classifyVersion(tc.v, tc.prev)
			if kind != tc.wantKind || tool != tc.wantTool {
				t.Fatalf("got (%q, %q), want (%q, %q)", kind, tool, tc.wantKind, tc.wantTool)
			}
			hv := historyVersion{Version: tc.v, Kind: kind, Tool: tool}
			if hv.external() != tc.wantExt {
				t.Errorf("external() = %v, want %v", hv.external(), tc.wantExt)
			}
		})
	}
}

func TestClassifyHistoryReconfigured(t *testing.T) {
	base := remote.Config{CheckpointHandledThreshold: 4000, MaxWriteBatchLength: 500}
	tuned := base
	tuned.CheckpointHandledThreshold = 1234
	tuned.CheckpointAfterMs = 9999
	def := func(c remote.Config) *remote.Definition {
		return &remote.Definition{Query: "q", EngineVersion: 1, Enabled: true, Config: c, Time: histTime}
	}
	hist := classifyHistory([]remote.Version{
		{Number: 1, Definition: def(tuned)},
		{Number: 0, Definition: def(base), Ledger: gafferLedger(remote.OpDeploy)},
	})
	if hist[0].Kind != kindReconfigured {
		t.Fatalf("v1 kind = %q, want reconfigured", hist[0].Kind)
	}
	got := map[string]string{}
	for _, c := range hist[0].ConfigChanges {
		got[c.Label] = c.From + "->" + c.To
	}
	if got["handled threshold"] != "4000->1234" || got["checkpoint after"] != "default->9999ms" {
		t.Errorf("config changes = %v", got)
	}
}

// tombstone is a deleted version still carrying the definition it removed, the
// shape the server writes for a delete (and the first half of a recreate).
func tombstone(number int64, query string) remote.Version {
	return remote.Version{Number: number, Deleted: true, Definition: &remote.Definition{Query: query, EngineVersion: 1, Time: histTime}}
}

func TestCollapseHistoryFoldsRecreate(t *testing.T) {
	// The observed recreate sequence from an enabled projection: disable flip,
	// delete tombstone, stamped create. The bookends fold into the create.
	hist := collapseHistory(classifyHistory([]remote.Version{
		ver(3, "q", true, gafferLedger(remote.OpRecreate)),
		tombstone(2, "q"),
		ver(1, "q", false, nil),
		ver(0, "q", true, gafferLedger(remote.OpDeploy)),
	}))
	if len(hist) != 2 {
		t.Fatalf("got %d rows, want 2 (recreate + deploy): %+v", len(hist), kinds(hist))
	}
	if hist[0].Kind != kindRecreate || len(hist[0].Absorbed) != 2 {
		t.Fatalf("row 0 = %q with %d absorbed, want recreate with 2", hist[0].Kind, len(hist[0].Absorbed))
	}
	if hist[0].Absorbed[0].Kind != kindDeleted || hist[0].Absorbed[1].Kind != kindDisabled {
		t.Errorf("absorbed = %q, %q, want deleted then disabled", hist[0].Absorbed[0].Kind, hist[0].Absorbed[1].Kind)
	}
	if absorbedCount(hist) != 2 {
		t.Errorf("absorbedCount = %d, want 2", absorbedCount(hist))
	}
}

func TestCollapseHistoryAlreadyDisabled(t *testing.T) {
	// Recreating an already-disabled projection: its disable step lands as a
	// rewritten no-op. That folds; the operator's own earlier disable does not.
	hist := collapseHistory(classifyHistory([]remote.Version{
		ver(4, "q", true, gafferLedger(remote.OpRecreate)),
		tombstone(3, "q"),
		ver(2, "q", false, nil), // recreate's no-op disable (already disabled)
		ver(1, "q", false, nil), // the operator's manual disable
		ver(0, "q", true, gafferLedger(remote.OpDeploy)),
	}))
	if len(hist) != 3 {
		t.Fatalf("got %d rows, want 3 (recreate + disabled + deploy): %v", len(hist), kinds(hist))
	}
	if len(hist[0].Absorbed) != 2 || hist[0].Absorbed[1].Kind != kindRewritten {
		t.Fatalf("row 0 absorbed = %v, want tombstone + the rewritten no-op", kinds(hist[0].Absorbed))
	}
	if hist[1].Kind != kindDisabled {
		t.Errorf("row 1 = %q, want the manual disable left visible", hist[1].Kind)
	}
}

func TestHistoryRowsFoldsBeforeLimit(t *testing.T) {
	// --limit counts displayed rows on the human timeline: folding first means a
	// recreate's bookends can't eat the limit and then vanish, dropping rows the
	// read window had room for (here the deploy).
	hist := classifyHistory([]remote.Version{
		ver(3, "q", true, gafferLedger(remote.OpRecreate)),
		tombstone(2, "q"),
		ver(1, "q", false, nil),
		ver(0, "q", true, gafferLedger(remote.OpDeploy)),
	})
	rows := historyRows(hist, 2)
	if len(rows) != 2 || rows[0].Kind != kindRecreate || rows[1].Kind != kindDeploy {
		t.Fatalf("rows = %v, want [recreate deploy]", kinds(rows))
	}
}

func TestCollapseHistoryConsecutiveRecreates(t *testing.T) {
	// Two recreates back to back: each folds its own bookends, and the skip-ahead
	// lands exactly on the next recreate rather than eating into its sequence.
	hist := collapseHistory(classifyHistory([]remote.Version{
		ver(6, "q", true, gafferLedger(remote.OpRecreate)),
		tombstone(5, "q"),
		ver(4, "q", false, nil),
		ver(3, "q", true, gafferLedger(remote.OpRecreate)),
		tombstone(2, "q"),
		ver(1, "q", false, nil),
		ver(0, "q", true, gafferLedger(remote.OpDeploy)),
	}))
	if len(hist) != 3 {
		t.Fatalf("got %d rows, want 3 (recreate + recreate + deploy): %v", len(hist), kinds(hist))
	}
	for i := range 2 {
		if hist[i].Kind != kindRecreate || len(hist[i].Absorbed) != 2 {
			t.Errorf("row %d = %q with %d absorbed, want a recreate folding both bookends", i, hist[i].Kind, len(hist[i].Absorbed))
		}
	}
	if hist[2].Kind != kindDeploy {
		t.Errorf("row 2 = %q, want the deploy", hist[2].Kind)
	}
}

func TestCollapseHistoryLeavesNonPatterns(t *testing.T) {
	t.Run("no tombstone below the recreate", func(t *testing.T) {
		// An interleaved write between the create and its bookends breaks the
		// pattern; nothing folds and every row stays visible.
		hist := collapseHistory(classifyHistory([]remote.Version{
			ver(3, "q", true, gafferLedger(remote.OpRecreate)),
			ver(2, "other", true, nil),
			tombstone(1, "q"),
			ver(0, "q", true, gafferLedger(remote.OpDeploy)),
		}))
		if len(hist) != 4 {
			t.Fatalf("got %d rows, want all 4 kept: %v", len(hist), kinds(hist))
		}
	})
	t.Run("bookends beyond the loaded window", func(t *testing.T) {
		// The recreate is the oldest loaded row; its bookends fold only once a
		// later page brings them in.
		hist := collapseHistory(classifyHistory([]remote.Version{
			ver(5, "q", true, gafferLedger(remote.OpRecreate)),
		}))
		if len(hist) != 1 || len(hist[0].Absorbed) != 0 {
			t.Fatalf("got %d rows with %d absorbed, want the bare recreate", len(hist), len(hist[0].Absorbed))
		}
	})
	t.Run("tombstone only", func(t *testing.T) {
		// A tombstone directly below folds even when the row after it isn't the
		// disable (e.g. it sits past a page boundary or an interleaved write).
		hist := collapseHistory(classifyHistory([]remote.Version{
			ver(3, "q", true, gafferLedger(remote.OpRecreate)),
			tombstone(2, "q"),
			ver(1, "q2", true, gafferLedger(remote.OpDeploy)),
		}))
		if len(hist) != 2 || len(hist[0].Absorbed) != 1 {
			t.Fatalf("got %d rows with %d absorbed, want tombstone folded alone", len(hist), len(hist[0].Absorbed))
		}
	})
}

func kinds(hist []historyVersion) []versionKind {
	out := make([]versionKind, len(hist))
	for i, hv := range hist {
		out[i] = hv.Kind
	}
	return out
}

func TestClassifyHistoryShortHash(t *testing.T) {
	hist := classifyHistory([]remote.Version{ver(1, "fromAll()", true, gafferLedger(remote.OpDeploy))})
	if len(hist[0].Hash) != 7 {
		t.Fatalf("Hash = %q, want a 7-char short hash", hist[0].Hash)
	}
}

func TestClassifyHistoryRevertedContentSharesHash(t *testing.T) {
	// An external edit then a revert to the original query: v2 and v0 share a hash,
	// the signal a rollback/revert landed identical content.
	hist := classifyHistory([]remote.Version{
		ver(2, "original", true, gafferLedger(remote.OpDeploy)),
		ver(1, "tampered", true, nil),
		ver(0, "original", true, gafferLedger(remote.OpDeploy)),
	})
	if hist[0].Hash != hist[2].Hash {
		t.Errorf("v2 hash %q != v0 hash %q, want equal (same content)", hist[0].Hash, hist[2].Hash)
	}
	if hist[1].Kind != kindEditedExternally {
		t.Errorf("v1 kind = %q, want edited externally", hist[1].Kind)
	}
}

func TestHistoryJSONKeepsRecreateBookends(t *testing.T) {
	// --json is the stream-fidelity view: a recreate stays one entry per write
	// (create / tombstone / disable), uncollapsed, with the create's kind named.
	hist := classifyHistory([]remote.Version{
		ver(3, "q", true, gafferLedger(remote.OpRecreate)),
		tombstone(2, "q"),
		ver(1, "q", false, nil),
		ver(0, "q", true, gafferLedger(remote.OpDeploy)),
	})
	var buf bytes.Buffer
	if err := renderHistoryJSON(&buf, hist); err != nil {
		t.Fatalf("renderHistoryJSON: %v", err)
	}
	var got []historyJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d entries, want all 4 writes", len(got))
	}
	if got[0].Kind != "recreate" || got[0].Operation != "recreate" {
		t.Errorf("entry 0 = kind %q operation %q, want recreate", got[0].Kind, got[0].Operation)
	}
	if !got[1].Deleted || got[2].Kind != "disabled" {
		t.Errorf("bookends = %+v, %+v, want the tombstone and disable kept", got[1], got[2])
	}
}

func TestRenderHistoryJSON(t *testing.T) {
	hist := classifyHistory([]remote.Version{
		ver(1, "v1", true, gafferLedger(remote.OpDeploy)),
		ver(0, "v0", true, nil),
	})
	var buf bytes.Buffer
	if err := renderHistoryJSON(&buf, hist); err != nil {
		t.Fatalf("renderHistoryJSON: %v", err)
	}
	var got []historyJSON
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].Version != 1 || got[0].Kind != "deploy" || got[0].External {
		t.Errorf("entry 0 = %+v", got[0])
	}
	if len(got[0].ContentHash) != 64 {
		t.Errorf("ContentHash = %q, want the full 64-char hash in JSON", got[0].ContentHash)
	}
	if got[0].Tool != remote.ToolName || got[0].Actor != "george@kurrent.io" || got[0].Revision != "9f8e7d6" {
		t.Errorf("entry 0 metadata = %+v", got[0])
	}
	if got[0].Time != histTime.Format(time.RFC3339) {
		t.Errorf("Time = %q, want RFC3339 %q", got[0].Time, histTime.Format(time.RFC3339))
	}
	// The metadata-less create carries no tool fields (omitempty).
	if got[1].Kind != "created" || got[1].Tool != "" {
		t.Errorf("entry 1 = %+v", got[1])
	}
}

func TestWriteHistory(t *testing.T) {
	hist := classifyHistory([]remote.Version{
		ver(3, "c", true, nil),                           // edited externally (content changed, no metadata)
		ver(2, "b", true, gafferLedger(remote.OpDeploy)), // deploy
		ver(1, "b", false, nil),                          // disabled: same content as v0, enabled flipped off
		ver(0, "b", true, gafferLedger(remote.OpDeploy)),
	})
	var buf bytes.Buffer
	newTextWriter(&buf, &buf).WriteHistory("orders", hist, 12)
	out := buf.String()
	for _, want := range []string{
		"deploy", "george@kurrent.io", "Gaffer 1.4.0", "src 9f8e7d6",
		"edited externally", "⚠ query changed outside gaffer",
		"disabled", // state change leads with the state word
		"Showing 4 of 12 entries",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
}

func TestWriteHistoryRevertGraph(t *testing.T) {
	// A revert nested inside a revert: content P at rows 0 and 6, content R at rows 2
	// and 4. The graph draws an outer bracket wrapping an inner one, each with rounded
	// fork/rejoin corners, and a dotted bridge alongside the detour.
	hist := classifyHistory([]remote.Version{
		ver(6, "P", true, gafferLedger(remote.OpDeploy)),
		ver(5, "Q", true, gafferLedger(remote.OpDeploy)),
		ver(4, "R", true, gafferLedger(remote.OpDeploy)),
		ver(3, "S", true, gafferLedger(remote.OpDeploy)),
		ver(2, "R", true, gafferLedger(remote.OpDeploy)),
		ver(1, "T", true, gafferLedger(remote.OpDeploy)),
		ver(0, "P", true, gafferLedger(remote.OpDeploy)),
	})
	if hist[0].Hash != hist[6].Hash || hist[2].Hash != hist[4].Hash {
		t.Fatalf("expected P and R to each recur: %q..%q, %q..%q", hist[0].Hash, hist[6].Hash, hist[2].Hash, hist[4].Hash)
	}
	var buf bytes.Buffer
	newTextWriter(&buf, &buf).WriteHistory("orders", hist, 7)
	out := buf.String()
	for _, want := range []string{
		"╰─╮", // a fork (outer and inner)
		"╭─╯", // a rejoin
		"┆",   // the dotted bridge alongside a detour
	} {
		if !strings.Contains(out, want) {
			t.Errorf("graph missing %q\n---\n%s", want, out)
		}
	}
	// The nested inner bracket sits one lane in, so a fork is drawn behind a bridge.
	if !strings.Contains(out, "┆ ╰─╮") {
		t.Errorf("expected an inner fork nested behind the outer bridge\n---\n%s", out)
	}
}
