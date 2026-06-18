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
	for _, tc := range []struct {
		name string
		e    diffEntry
		want string
	}{
		{"in sync", diffEntry{Name: "a", State: stateInSync}, "a: in sync"},
		{"not deployed", diffEntry{Name: "b", State: stateNotDeployed, Local: desc("q", 2, false)}, "b: not deployed (local only)"},
		{"untracked", diffEntry{Name: "c", State: stateUntracked}, "c: untracked (deployed, not in gaffer.toml)"},
		{
			"drifted",
			diffEntry{
				Name:     "d",
				State:    stateDrifted,
				Cmp:      deploy.Comparison{QueryDiffers: true, EngineVersionDiffers: true},
				Local:    desc("x", 2, false),
				Deployed: desc("y", 1, false),
			},
			"d: drifted (query, engine version: remote=1 local=2)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b bytes.Buffer
			renderDiffText(&b, tc.e)
			if got := strings.TrimSpace(b.String()); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDriftReasonsLabelDirection(t *testing.T) {
	e := diffEntry{
		Cmp:      deploy.Comparison{EmitDiffers: true, TrackEmittedStreamsDiffers: true},
		Local:    &deploy.Descriptor{Emit: true, TrackEmittedStreams: true},
		Deployed: &deploy.Descriptor{Emit: false, TrackEmittedStreams: false},
	}
	got := strings.Join(driftReasons(e), ", ")
	for _, want := range []string{"emit: remote=false local=true", "track emitted streams: remote=false local=true"} {
		if !strings.Contains(got, want) {
			t.Errorf("reasons = %q, want it to contain %q", got, want)
		}
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
