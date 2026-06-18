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

func TestRenderDiffText(t *testing.T) {
	entries := []diffEntry{
		{Name: "a", State: stateInSync},
		{Name: "b", State: stateNotDeployed, Local: desc("q", 2, false)},
		{Name: "c", State: stateUntracked},
		{
			Name:     "d",
			State:    stateDrifted,
			Cmp:      deploy.Comparison{QueryDiffers: true, EngineVersionDiffers: true},
			Local:    desc("x", 2, false),
			Deployed: desc("y", 1, false),
		},
	}
	var b bytes.Buffer
	renderDiffText(&b, entries)
	out := b.String()
	for _, want := range []string{
		"a: in sync",
		"b: not deployed",
		"c: untracked (deployed, not in gaffer.toml)",
		"d: drifted (query, engine version 1 -> 2)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestDriftReasonsDirectionIsDeployedToLocal(t *testing.T) {
	e := diffEntry{
		Cmp:      deploy.Comparison{EmitDiffers: true, TrackEmittedStreamsDiffers: true},
		Local:    &deploy.Descriptor{Emit: true, TrackEmittedStreams: true},
		Deployed: &deploy.Descriptor{Emit: false, TrackEmittedStreams: false},
	}
	got := strings.Join(driftReasons(e), ", ")
	for _, want := range []string{"emit false -> true", "track emitted streams false -> true"} {
		if !strings.Contains(got, want) {
			t.Errorf("reasons = %q, want it to contain %q", got, want)
		}
	}
}

func TestRenderDiffJSON(t *testing.T) {
	entries := []diffEntry{
		{Name: "synced", State: stateInSync, Local: desc("q", 2, true), Deployed: desc("q", 2, true)},
		{
			Name:     "drifted",
			State:    stateDrifted,
			Cmp:      deploy.Comparison{QueryDiffers: true},
			Local:    desc("x", 2, false),
			Deployed: desc("y", 2, false),
		},
		{Name: "untracked", State: stateUntracked},
	}
	var b bytes.Buffer
	if err := renderDiffJSON(&b, entries); err != nil {
		t.Fatalf("renderDiffJSON: %v", err)
	}
	var got []diffJSON
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, b.String())
	}
	if len(got) != 3 {
		t.Fatalf("want 3 entries, got %d", len(got))
	}

	synced := got[0]
	if synced.State != "in-sync" || synced.LocalHash == "" || synced.LocalHash != synced.DeployedHash {
		t.Errorf("synced entry = %+v; want matching non-empty hashes", synced)
	}
	if synced.Drift != nil {
		t.Errorf("in-sync entry should carry no drift")
	}

	drifted := got[1]
	if drifted.Drift == nil || !drifted.Drift.Query {
		t.Errorf("drifted entry should report query drift, got %+v", drifted)
	}
	if drifted.LocalHash == drifted.DeployedHash {
		t.Errorf("drifted entry hashes should differ")
	}

	untracked := got[2]
	if untracked.State != "untracked" || untracked.LocalHash != "" || untracked.DeployedHash != "" || untracked.Drift != nil {
		t.Errorf("untracked entry = %+v; want bare state", untracked)
	}
}
