package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

func TestProgressText(t *testing.T) {
	for _, tc := range []struct {
		e    statusEntry
		want string
	}{
		{statusEntry{}, "-"}, // not deployed
		{statusEntry{runtime: &remote.Status{Progress: 100}}, "100%"},
		{statusEntry{runtime: &remote.Status{Progress: 42}}, "42%"},
		{statusEntry{runtime: &remote.Status{Progress: 0}}, "0%"},
		{statusEntry{runtime: &remote.Status{Progress: -1}}, "unknown"},
		{statusEntry{runtime: &remote.Status{Progress: -2}}, "unknown"},
	} {
		if got := progressText(tc.e); got != tc.want {
			t.Errorf("progressText = %q, want %q", got, tc.want)
		}
	}
}

func TestWriteStatusTable(t *testing.T) {
	entries := []statusEntry{
		{comparison: comparison{Name: "count", State: driftInSync}, runtime: &remote.Status{State: remote.StateRunning, Progress: 100}},
		{comparison: comparison{Name: "orders", State: driftNotDeployed}},
		{comparison: comparison{Name: "legacy", State: driftUntracked}, runtime: &remote.Status{State: remote.StateRunning, Progress: 100}},
		// An untracked projection carrying gaffer's tool metadata: the verdict is orphan,
		// and the provenance columns carry its last-deploy date and tool.
		{comparison: comparison{Name: "removed", State: driftUntracked, Ledger: ledgerEntry(remote.ToolName, "admin")}, runtime: &remote.Status{State: remote.StateRunning, Progress: 100}},
		// A metadata-less projection (no ledger): the last-write date still shows from
		// the deployed event, but the "via" column is empty (no tool to name).
		{comparison: comparison{Name: "adhoc", State: driftUntracked, DeployedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}, runtime: &remote.Status{State: remote.StateRunning, Progress: 100}},
		// A broken local projection that's still running on the server: the row
		// shows its runtime state, with drift "invalid" rather than aborting.
		{comparison: comparison{Name: "broken", State: driftInvalid, LocalErr: errors.New("nope")}, runtime: &remote.Status{State: remote.StateRunning, Progress: 100}},
	}
	var b bytes.Buffer
	newTextWriter(&b, &b).WriteStatusTable(entries)
	out := b.String()

	for _, want := range []string{
		"PROJECTION", "STATE", "PROGRESS", "LAST DEPLOY", "DEPLOYED VIA", "DRIFT",
		"count", "running", "100%", "in sync",
		"orders", "not deployed",
		"legacy", "untracked",
		"removed", "orphan", "Gaffer", "2026-06-29",
		"adhoc", "2026-05-01",
		"broken", "invalid",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if line != strings.TrimRight(line, " ") {
			t.Errorf("row has trailing whitespace: %q", line)
		}
	}

	// The count row's cells must appear left-to-right in column order, so a
	// column swap is caught (substring presence alone wouldn't).
	var countLine string
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "count") {
			countLine = line
			break
		}
	}
	prev := -1
	for _, cell := range []string{"count", "running", "100%", "in sync"} {
		i := strings.Index(countLine, cell)
		if i <= prev {
			t.Fatalf("cell %q out of column order in row %q", cell, countLine)
		}
		prev = i
	}
}

func TestWriteStatusBlock(t *testing.T) {
	render := func(e statusEntry) string {
		var b bytes.Buffer
		newTextWriter(&b, &b).WriteStatus(e)
		return b.String()
	}

	running := render(statusEntry{
		comparison: comparison{Name: "count", State: driftInSync},
		runtime:    &remote.Status{State: remote.StateRunning, Progress: 100, Position: "C:120/P:118"},
	})
	for _, want := range []string{"count", "State: running", "Progress: 100%", "Position: C:120/P:118", "Drift: in sync"} {
		if !strings.Contains(running, want) {
			t.Errorf("missing %q in:\n%s", want, running)
		}
	}

	faulted := render(statusEntry{
		comparison: comparison{Name: "bad", State: driftDrifted},
		runtime:    &remote.Status{State: remote.StateFaulted, FaultReason: "boom"},
	})
	if !strings.Contains(faulted, "Fault: boom") {
		t.Errorf("faulted block missing fault reason:\n%s", faulted)
	}

	notDeployed := render(statusEntry{comparison: comparison{Name: "orders", State: driftNotDeployed}})
	if !strings.Contains(notDeployed, "Drift: not deployed") || strings.Contains(notDeployed, "State:") || strings.Contains(notDeployed, "Last deploy") {
		t.Errorf("not-deployed block should show drift only, no provenance:\n%s", notDeployed)
	}

	invalid := render(statusEntry{
		comparison: comparison{Name: "broken", State: driftInvalid, LocalErr: errors.New("Unexpected token (3:5)")},
		runtime:    &remote.Status{State: remote.StateRunning, Progress: 100},
	})
	for _, want := range []string{"State: running", "Drift: invalid", "Unexpected token (3:5)"} {
		if !strings.Contains(invalid, want) {
			t.Errorf("invalid block missing %q in:\n%s", want, invalid)
		}
	}

	// Untracked owned by another tool: the verdict is plain "untracked", and the
	// provenance block names the managing tool + when.
	foreign := render(statusEntry{
		comparison: comparison{Name: "legacy", State: driftUntracked, Ledger: ledgerEntry("KurrentDB Embedded UI", "jane")},
		runtime:    &remote.Status{State: remote.StateRunning, Progress: 100},
	})
	for _, want := range []string{"Drift: untracked", "Deployed via: KurrentDB Embedded UI", "Deployer: jane", "Last deploy: 2026-06-29"} {
		if !strings.Contains(foreign, want) {
			t.Errorf("foreign block missing %q in:\n%s", want, foreign)
		}
	}

	// Metadata-less untracked: the last-write date shows (from the deployed event),
	// but with no tool entry there's no "Deployed via" / "Deployer".
	adhoc := render(statusEntry{
		comparison: comparison{Name: "adhoc", State: driftUntracked, DeployedAt: time.Date(2026, 5, 1, 9, 30, 0, 0, time.UTC)},
		runtime:    &remote.Status{State: remote.StateRunning, Progress: 100},
	})
	if !strings.Contains(adhoc, "Last deploy: 2026-05-01") || strings.Contains(adhoc, "Deployed via") {
		t.Errorf("adhoc block should show the last-write date but no tool:\n%s", adhoc)
	}

	// Drifted but the server still matches my last deploy: verdict "local ahead",
	// with the provenance block carrying the deployer/date behind it.
	ahead := render(statusEntry{
		comparison: comparison{
			Name: "count", State: driftDrifted,
			Deployed: desc("a", 2, false), DeployBaseline: desc("a", 2, false),
			Ledger: ledgerEntry(remote.ToolName, "admin"),
		},
		runtime: &remote.Status{State: remote.StateRunning, Progress: 100},
	})
	for _, want := range []string{"Drift: local ahead", "Deployed via: Gaffer", "Deployer: admin", "Last deploy: 2026-06-29"} {
		if !strings.Contains(ahead, want) {
			t.Errorf("local-ahead block missing %q in:\n%s", want, ahead)
		}
	}

	// Full ledger: the version is appended to the tool, and the revision is
	// abbreviated to 12 chars by shortRevision.
	full := render(statusEntry{
		comparison: comparison{
			Name: "rich", State: driftUntracked,
			Ledger: &remote.Ledger{Tool: remote.ToolName, ToolVersion: "1.2.3", Actor: "alice", Revision: "f24668b68c210dbbf002a2807dc3ce735c2ea9af", Time: time.Date(2026, 6, 29, 9, 38, 0, 0, time.UTC)},
		},
		runtime: &remote.Status{State: remote.StateRunning, Progress: 100},
	})
	for _, want := range []string{"Deployed via: Gaffer 1.2.3", "Deployer: alice", "Revision: f24668b68c21"} {
		if !strings.Contains(full, want) {
			t.Errorf("full-provenance block missing %q in:\n%s", want, full)
		}
	}

	// An unreadable ledger flags "Deploy metadata: unreadable" and shows no tool lines.
	unreadable := render(statusEntry{
		comparison: comparison{Name: "bad", State: driftUntracked, LedgerErr: remote.ErrMalformedLedger},
		runtime:    &remote.Status{State: remote.StateRunning, Progress: 100},
	})
	if !strings.Contains(unreadable, "Deploy metadata: unreadable") || strings.Contains(unreadable, "Deployed via") {
		t.Errorf("unreadable block should flag unreadable metadata and show no tool:\n%s", unreadable)
	}
}

func TestDriftStyle(t *testing.T) {
	tw := newTextWriter(io.Discard, io.Discard)
	for _, tc := range []struct {
		name string
		c    comparison
		want lipgloss.Style
	}{
		{"in-sync", comparison{State: driftInSync}, tw.styles.added},
		{"invalid", comparison{State: driftInvalid}, tw.styles.errStatus},
		{"not-deployed", comparison{State: driftNotDeployed}, tw.styles.muted},
		{"plain untracked", comparison{State: driftUntracked}, tw.styles.muted},
		{"orphan", comparison{State: driftUntracked, Ledger: ledgerEntry(remote.ToolName, "")}, tw.styles.warning},
		{"drifted", comparison{State: driftDrifted}, tw.styles.warning},
	} {
		if got := tw.driftStyle(tc.c).GetForeground(); got != tc.want.GetForeground() {
			t.Errorf("%s: driftStyle foreground = %v, want %v", tc.name, got, tc.want.GetForeground())
		}
	}
}

func TestRenderStatusJSON(t *testing.T) {
	entries := []statusEntry{
		{comparison: comparison{Name: "count", State: driftInSync}, runtime: &remote.Status{State: remote.StateRunning, Progress: 100, Position: "C:1"}},
		{comparison: comparison{Name: "orders", State: driftNotDeployed}},
		{comparison: comparison{Name: "broken", State: driftInvalid, LocalErr: errors.New("Unexpected token (3:5)")}, runtime: &remote.Status{State: remote.StateRunning}},
		{comparison: comparison{Name: "orphaned", State: driftUntracked, Ledger: ledgerEntry(remote.ToolName, "admin")}, runtime: &remote.Status{State: remote.StateRunning}},
		// Metadata-less: lastDeployed (event time) is present even though lastWrite isn't.
		{comparison: comparison{Name: "adhoc", State: driftUntracked, DeployedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)}, runtime: &remote.Status{State: remote.StateRunning}},
	}
	var b bytes.Buffer
	if err := renderStatusJSON(&b, entries); err != nil {
		t.Fatalf("renderStatusJSON: %v", err)
	}
	var got []statusJSON
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, b.String())
	}
	if len(got) != 5 {
		t.Fatalf("want 5 entries, got %d", len(got))
	}
	// owner is always present, including the in-config value for a tracked projection.
	if got[0].Drift != "in-sync" || got[0].Owner != "in-config" || got[0].Runtime == nil || got[0].Runtime.State != "running" {
		t.Errorf("count entry = %+v", got[0])
	}
	if got[1].Drift != "not-deployed" || got[1].Owner != "in-config" || got[1].Runtime != nil || got[1].Error != "" {
		t.Errorf("orders should carry drift only, got %+v", got[1])
	}
	if got[2].Drift != "invalid" || got[2].Error != "Unexpected token (3:5)" {
		t.Errorf("broken entry should carry the compile error, got %+v", got[2])
	}
	// The orphan carries its ownership, the deploy time, and the tool behind it.
	if got[3].Owner != "orphan" || got[3].LastDeployed == "" || got[3].LastWrite == nil || got[3].LastWrite.Tool != remote.ToolName {
		t.Errorf("orphaned entry = %+v (lastWrite %+v); want owner orphan + deploy time + gaffer last-write", got[3], got[3].LastWrite)
	}
	// Metadata-less: the event-derived deploy time is present, with no tool attribution.
	if got[4].Owner != "unknown" || got[4].LastDeployed == "" || got[4].LastWrite != nil {
		t.Errorf("adhoc entry = %+v (lastWrite %+v); want owner unknown + deploy time + no last-write", got[4], got[4].LastWrite)
	}
}

func TestShortRevision(t *testing.T) {
	const full = "f24668b68c210dbbf002a2807dc3ce735c2ea9af"
	for _, tc := range []struct{ in, want string }{
		{full, "f24668b68c21"},                      // full SHA -> 12 chars
		{full + "+changes", "f24668b68c21+changes"}, // dirty marker preserved
		{"v1.2.3", "v1.2.3"},                        // custom non-SHA -> untouched
		{"main", "main"},
		{"", ""},
	} {
		if got := shortRevision(tc.in); got != tc.want {
			t.Errorf("shortRevision(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
