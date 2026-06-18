package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
)

func desc(query string, engineVersion int, emit bool) *deploy.Descriptor {
	return &deploy.Descriptor{Query: query, EngineVersion: engineVersion, Emit: emit}
}

// renderWriteDiff captures WriteDiff's output. lipgloss renders plain (no ANSI)
// to a buffer, so assertions can match on substrings.
func renderWriteDiff(e diffEntry) string {
	var b bytes.Buffer
	newTextWriter(&b, &b).WriteDiff(e)
	return b.String()
}

func TestWriteDiffInSync(t *testing.T) {
	out := renderWriteDiff(diffEntry{
		Name: "count", State: stateInSync,
		Local: desc("q", 2, false), Deployed: desc("q", 2, false),
	})
	for _, want := range []string{"count", "Query: in sync", "Engine version: 2", "Emit: disabled"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestWriteDiffDrifted(t *testing.T) {
	out := renderWriteDiff(diffEntry{
		Name:  "count",
		State: stateDrifted,
		Cmp:   deploy.Comparison{QueryDiffers: true, EngineVersionDiffers: true},
		// remote one line, local three -> +2 -0.
		Deployed: desc("a\n", 1, false),
		Local:    desc("a\nb\nc\n", 2, false),
	})
	for _, want := range []string{"Query: +2 -0", "Engine version: remote 1, local 2", "Emit: disabled"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestWriteDiffEmitAndTrackDrift(t *testing.T) {
	out := renderWriteDiff(diffEntry{
		Name:     "count",
		State:    stateDrifted,
		Cmp:      deploy.Comparison{EmitDiffers: true, TrackEmittedStreamsDiffers: true},
		Deployed: &deploy.Descriptor{EngineVersion: 1, Emit: false, TrackEmittedStreams: false},
		Local:    &deploy.Descriptor{EngineVersion: 1, Emit: true, TrackEmittedStreams: true},
	})
	for _, want := range []string{"Emit: remote disabled, local enabled", "Track emitted streams: remote disabled, local enabled"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestWriteDiffOneSided(t *testing.T) {
	if out := renderWriteDiff(diffEntry{Name: "orders", State: stateNotDeployed, Local: desc("q", 2, false)}); !strings.Contains(out, "orders") || !strings.Contains(out, "not deployed (local only)") {
		t.Errorf("not-deployed render:\n%s", out)
	}
	if out := renderWriteDiff(diffEntry{Name: "legacy", State: stateUntracked, Deployed: desc("q", 2, false)}); !strings.Contains(out, "untracked (deployed, not in gaffer.toml)") {
		t.Errorf("untracked render:\n%s", out)
	}
}

func TestRenderDiffJSON(t *testing.T) {
	decode := func(e diffEntry) diffJSON {
		t.Helper()
		var b bytes.Buffer
		if err := renderDiffJSON(&b, e); err != nil {
			t.Fatalf("renderDiffJSON: %v", err)
		}
		var j diffJSON
		if err := json.Unmarshal(b.Bytes(), &j); err != nil {
			t.Fatalf("unmarshal: %v\n%s", err, b.String())
		}
		return j
	}

	synced := decode(diffEntry{Name: "s", State: stateInSync, Local: desc("q", 2, true), Deployed: desc("q", 2, true)})
	if synced.State != "in-sync" || synced.LocalHash == "" || synced.LocalHash != synced.DeployedHash || synced.Drift != nil {
		t.Errorf("synced = %+v; want matching non-empty hashes, no drift", synced)
	}

	drifted := decode(diffEntry{
		Name:     "d",
		State:    stateDrifted,
		Cmp:      deploy.Comparison{QueryDiffers: true},
		Local:    desc("x", 2, false),
		Deployed: desc("y", 2, false),
	})
	if drifted.Drift == nil || !drifted.Drift.Query || drifted.LocalHash == drifted.DeployedHash {
		t.Errorf("drifted = %+v; want query drift and differing hashes", drifted)
	}

	untracked := decode(diffEntry{Name: "u", State: stateUntracked, Deployed: desc("q", 2, false)})
	if untracked.State != "untracked" || untracked.LocalHash != "" || untracked.DeployedHash == "" || untracked.Drift != nil {
		t.Errorf("untracked = %+v; want deployed hash only", untracked)
	}
}
