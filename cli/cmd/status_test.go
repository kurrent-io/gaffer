package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

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
		// A broken local projection that's still running on the server: the row
		// shows its runtime state, with drift "invalid" rather than aborting.
		{comparison: comparison{Name: "broken", State: driftInvalid, LocalErr: errors.New("nope")}, runtime: &remote.Status{State: remote.StateRunning, Progress: 100}},
	}
	var b bytes.Buffer
	newTextWriter(&b, &b).WriteStatusTable(entries)
	out := b.String()

	for _, want := range []string{
		"PROJECTION", "STATE", "PROGRESS", "DRIFT",
		"count", "running", "100%", "in sync",
		"orders", "not deployed",
		"legacy", "untracked",
		"broken", "invalid",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line != strings.TrimRight(line, " ") {
			t.Errorf("row has trailing whitespace: %q", line)
		}
	}

	// The count row's cells must appear left-to-right in column order, so a
	// column swap is caught (substring presence alone wouldn't).
	var countLine string
	for _, line := range strings.Split(out, "\n") {
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
	if !strings.Contains(notDeployed, "Drift: not deployed (local only)") || strings.Contains(notDeployed, "State:") {
		t.Errorf("not-deployed block should show the spelled-out drift only:\n%s", notDeployed)
	}

	invalid := render(statusEntry{
		comparison: comparison{Name: "broken", State: driftInvalid, LocalErr: errors.New("Unexpected token (3:5)")},
		runtime:    &remote.Status{State: remote.StateRunning, Progress: 100},
	})
	for _, want := range []string{"State: running", "Drift: invalid (local definition)", "Unexpected token (3:5)"} {
		if !strings.Contains(invalid, want) {
			t.Errorf("invalid block missing %q in:\n%s", want, invalid)
		}
	}
}

func TestRenderStatusJSON(t *testing.T) {
	entries := []statusEntry{
		{comparison: comparison{Name: "count", State: driftInSync}, runtime: &remote.Status{State: remote.StateRunning, Progress: 100, Position: "C:1"}},
		{comparison: comparison{Name: "orders", State: driftNotDeployed}},
		{comparison: comparison{Name: "broken", State: driftInvalid, LocalErr: errors.New("Unexpected token (3:5)")}, runtime: &remote.Status{State: remote.StateRunning}},
	}
	var b bytes.Buffer
	if err := renderStatusJSON(&b, entries); err != nil {
		t.Fatalf("renderStatusJSON: %v", err)
	}
	var got []statusJSON
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, b.String())
	}
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d", len(got))
	}
	if got[0].Drift != "in-sync" || got[0].Runtime == nil || got[0].Runtime.State != "running" {
		t.Errorf("count entry = %+v", got[0])
	}
	if got[1].Drift != "not-deployed" || got[1].Runtime != nil || got[1].Error != "" {
		t.Errorf("orders should carry drift only, got %+v", got[1])
	}
	if got[2].Drift != "invalid" || got[2].Error != "Unexpected token (3:5)" {
		t.Errorf("broken entry should carry the compile error, got %+v", got[2])
	}
}
