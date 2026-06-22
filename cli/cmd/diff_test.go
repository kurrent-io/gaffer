package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
)

func desc(query string, engineVersion int, emit bool) *deploy.Descriptor {
	return &deploy.Descriptor{Query: query, EngineVersion: engineVersion, Emit: emit}
}

// renderWriteDiff captures WriteDiff's output. lipgloss renders plain (no ANSI)
// to a buffer, so assertions can match on substrings.
func renderWriteDiff(e comparison) string {
	var b bytes.Buffer
	newTextWriter(&b, &b).WriteDiff(e)
	return b.String()
}

func TestWriteDiffInSync(t *testing.T) {
	out := renderWriteDiff(comparison{
		Name: "count", State: driftInSync,
		Local: desc("q", 2, false), Deployed: desc("q", 2, false),
	})
	for _, want := range []string{"count", "Query: in sync", "Engine version: 2", "Emit: disabled"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestWriteDiffDrifted(t *testing.T) {
	out := renderWriteDiff(comparison{
		Name:  "count",
		State: driftDrifted,
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
	out := renderWriteDiff(comparison{
		Name:     "count",
		State:    driftDrifted,
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
	if out := renderWriteDiff(comparison{Name: "orders", State: driftNotDeployed, Local: desc("q", 2, false)}); !strings.Contains(out, "orders") || !strings.Contains(out, "not deployed (local only)") {
		t.Errorf("not-deployed render:\n%s", out)
	}
	if out := renderWriteDiff(comparison{Name: "legacy", State: driftUntracked, Deployed: desc("q", 2, false)}); !strings.Contains(out, "untracked (deployed, not in gaffer.toml)") {
		t.Errorf("untracked render:\n%s", out)
	}
}

func TestWriteDiffInvalid(t *testing.T) {
	// Local source doesn't compile but is deployed: the query and engine version
	// (no compile needed) still diff against the deployed side, emit is unknown,
	// and the compile error is shown.
	out := renderWriteDiff(comparison{
		Name:     "count",
		State:    driftInvalid,
		Cmp:      deploy.Comparison{QueryDiffers: true, EngineVersionDiffers: true},
		Deployed: desc("a\n", 1, true),
		Local:    desc("a\nb\n", 2, false), // partial: emit is not meaningful
		LocalErr: errors.New("Unexpected identifier 'state' (projection.js:7:11)"),
	})
	for _, want := range []string{
		"Query: +1 -0",
		"Engine version: remote 1, local 2",
		"Emit: unknown (local source does not compile)",
		"Unexpected identifier 'state'",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestWriteDiffInvalidNotDeployed(t *testing.T) {
	out := renderWriteDiff(comparison{
		Name:     "count",
		State:    driftInvalid,
		Local:    desc("a\n", 2, false),
		LocalErr: errors.New("Unexpected end of input"),
	})
	for _, want := range []string{"not deployed; local source does not compile", "Unexpected end of input"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderDiffJSON(t *testing.T) {
	decode := func(e comparison) diffJSON {
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

	synced := decode(comparison{Name: "s", State: driftInSync, Local: desc("q", 2, true), Deployed: desc("q", 2, true)})
	if synced.Drift != "in-sync" || synced.LocalHash == "" || synced.LocalHash != synced.DeployedHash || synced.Changes != nil {
		t.Errorf("synced = %+v; want matching non-empty hashes, no drift", synced)
	}

	drifted := decode(comparison{
		Name:     "d",
		State:    driftDrifted,
		Cmp:      deploy.Comparison{QueryDiffers: true},
		Local:    desc("x", 2, false),
		Deployed: desc("y", 2, false),
	})
	if drifted.Changes == nil || !drifted.Changes.Query || drifted.LocalHash == drifted.DeployedHash {
		t.Errorf("drifted = %+v; want query drift and differing hashes", drifted)
	}

	untracked := decode(comparison{Name: "u", State: driftUntracked, Deployed: desc("q", 2, false)})
	if untracked.Drift != "untracked" || untracked.LocalHash != "" || untracked.DeployedHash == "" || untracked.Changes != nil {
		t.Errorf("untracked = %+v; want deployed hash only", untracked)
	}

	// Invalid: report the compile error and the deployed hash, but no local hash
	// (emit can't be derived) and no changes verdict.
	invalid := decode(comparison{
		Name: "i", State: driftInvalid,
		Local: desc("q", 2, false), Deployed: desc("q", 2, false),
		LocalErr: errors.New("boom"),
	})
	if invalid.Drift != "invalid" || invalid.Error != "boom" || invalid.LocalHash != "" || invalid.DeployedHash == "" || invalid.Changes != nil {
		t.Errorf("invalid = %+v; want error + deployed hash only", invalid)
	}
}
