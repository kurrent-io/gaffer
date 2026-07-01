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
