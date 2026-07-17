package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/target"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

func TestReclassifyAuth(t *testing.T) {
	tripped := &engine.AuthInvalidation{}
	tripped.Trip()
	untripped := &engine.AuthInvalidation{}
	readErr := errors.New("read failed")

	// A read failure on a tripped credential becomes AuthRequiredError for env.
	got := reclassifyAuth(readErr, tripped, "prod")
	var authErr *target.AuthRequiredError
	if !errors.As(got, &authErr) {
		t.Fatalf("tripped read error: want *AuthRequiredError, got %T (%v)", got, got)
	}
	if authErr.Env != "prod" {
		t.Errorf("AuthRequiredError.Env: got %q want %q", authErr.Env, "prod")
	}

	// Untripped, nil handle, and nil error all pass through unchanged.
	if got := reclassifyAuth(readErr, untripped, "prod"); !errors.Is(got, readErr) {
		t.Errorf("untripped: want original error, got %v", got)
	}
	if got := reclassifyAuth(readErr, nil, "prod"); !errors.Is(got, readErr) {
		t.Errorf("nil authInv: want original error, got %v", got)
	}
	if got := reclassifyAuth(nil, tripped, "prod"); got != nil {
		t.Errorf("nil error: want nil (no success reclassified as auth), got %v", got)
	}
}

func TestLocalDiffDescriptorInvalidLocal(t *testing.T) {
	// A local projection that doesn't compile still yields its source for a
	// version diff, but with the compile error surfaced (not swallowed) and no
	// trustworthy hash.
	const broken = `fromAll().when({ $any: function (s, e) { return `
	p := testutil.NewProject(t).AddProjection("bad", broken).Save()

	d, compileErr, err := localDiffDescriptor(p.Cfg, p.Dir, "bad")
	if err != nil {
		t.Fatalf("localDiffDescriptor returned a fatal error for a mere compile failure: %v", err)
	}
	if compileErr == nil {
		t.Fatal("a non-compiling local must return its compile error, not swallow it")
	}
	if d.Query == "" {
		t.Error("the source should still be available to diff")
	}
}

func desc(query string, engineVersion int, emit bool) *deploy.Descriptor {
	return &deploy.Descriptor{Query: query, EngineVersion: engineVersion, Emit: emit}
}

// renderWriteDiff captures WriteDiff's output. lipgloss renders plain (no ANSI)
// to a buffer, so assertions can match on substrings.
func renderWriteDiff(e drift.Comparison) string {
	var b bytes.Buffer
	newTextWriter(&b, &b).WriteDiff(e)
	return b.String()
}

func TestWriteDiffInSync(t *testing.T) {
	out := renderWriteDiff(drift.Comparison{
		Name: "count", State: drift.InSync,
		Local: desc("q", 2, false), Deployed: desc("q", 2, false),
	})
	for _, want := range []string{"count", "Query: in sync", "Engine version: 2", "Emit: disabled"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestWriteDiffDrifted(t *testing.T) {
	out := renderWriteDiff(drift.Comparison{
		Name:  "count",
		State: drift.Drifted,
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
	out := renderWriteDiff(drift.Comparison{
		Name:     "count",
		State:    drift.Drifted,
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
	if out := renderWriteDiff(drift.Comparison{Name: "orders", State: drift.NotDeployed, Local: desc("q", 2, false)}); !strings.Contains(out, "orders") || !strings.Contains(out, "not deployed (local only)") {
		t.Errorf("not-deployed render:\n%s", out)
	}
	if out := renderWriteDiff(drift.Comparison{Name: "legacy", State: drift.Untracked, Deployed: desc("q", 2, false)}); !strings.Contains(out, "untracked (deployed, not in gaffer.toml)") {
		t.Errorf("untracked render:\n%s", out)
	}
}

func TestWriteDiffInvalid(t *testing.T) {
	// Local source doesn't compile but is deployed: the query and engine version
	// (no compile needed) still diff against the deployed side, emit is unknown,
	// and the compile error is shown.
	out := renderWriteDiff(drift.Comparison{
		Name:     "count",
		State:    drift.Invalid,
		Cmp:      deploy.Comparison{QueryDiffers: true, EngineVersionDiffers: true},
		Deployed: desc("a\n", 1, true),
		Local:    desc("a\nb\n", 2, false), // partial: emit is not meaningful
		LocalErr: errors.New("Unexpected identifier 'state' (projection.js:7:11)"),
	})
	for _, want := range []string{
		"Query: +1 -0",
		"Engine version: remote 1, local 2",
		"Emit: unknown (invalid local definition)",
		"Unexpected identifier 'state'",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestWriteDiffInvalidNotDeployed(t *testing.T) {
	out := renderWriteDiff(drift.Comparison{
		Name:     "count",
		State:    drift.Invalid,
		Local:    desc("a\n", 2, false),
		LocalErr: errors.New("Unexpected end of input"),
	})
	for _, want := range []string{"not deployed; invalid local definition", "Unexpected end of input"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestWriteDiffLedger(t *testing.T) {
	// Untracked carrying gaffer's tool entry renders as an orphan, with provenance.
	orphan := renderWriteDiff(drift.Comparison{Name: "legacy", State: drift.Untracked, Deployed: desc("q", 2, false), Ledger: ledgerEntry(remote.ToolName, "admin")})
	for _, want := range []string{"orphan (deployed, not in gaffer.toml)", "Deployed via: Gaffer", "Last deploy: 2026-06-29"} {
		if !strings.Contains(orphan, want) {
			t.Errorf("orphan render missing %q:\n%s", want, orphan)
		}
	}
	// A drifted projection whose deployed def still matches my last gaffer deploy:
	// verdict "local ahead", with the deployer/date in the provenance block.
	localAhead := renderWriteDiff(drift.Comparison{
		Name: "count", State: drift.Drifted, Cmp: deploy.Comparison{QueryDiffers: true},
		Deployed: desc("a\n", 1, false), Local: desc("b\n", 1, false),
		Ledger: ledgerEntry(remote.ToolName, "admin"), DeployBaseline: desc("a\n", 1, false),
	})
	for _, want := range []string{"Drift: local ahead", "Deployer: admin", "Last deploy: 2026-06-29"} {
		if !strings.Contains(localAhead, want) {
			t.Errorf("local-ahead render missing %q:\n%s", want, localAhead)
		}
	}
}

func TestRenderDiffJSON(t *testing.T) {
	decode := func(e drift.Comparison) diffJSON {
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

	synced := decode(drift.Comparison{Name: "s", State: drift.InSync, Local: desc("q", 2, true), Deployed: desc("q", 2, true)})
	if synced.Verdict.Drift != "in-sync" || synced.Right.Hash == "" || synced.Right.Hash != synced.Left.Hash || synced.Changes != nil {
		t.Errorf("synced = %+v (verdict %+v); want matching non-empty hashes, no drift", synced, synced.Verdict)
	}
	// The sides name themselves and carry the structured line diff regardless of state.
	if synced.Left.Ref != "deployed" || synced.Right.Ref != "local" || len(synced.Lines) == 0 {
		t.Errorf("synced sides = %+v / %+v, lines %d; want deployed/local with a line diff", synced.Left, synced.Right, len(synced.Lines))
	}

	drifted := decode(drift.Comparison{
		Name:     "d",
		State:    drift.Drifted,
		Cmp:      deploy.Comparison{QueryDiffers: true},
		Local:    desc("x", 2, false),
		Deployed: desc("y", 2, false),
	})
	if drifted.Changes == nil || !drifted.Changes.Query || drifted.Right.Hash == drifted.Left.Hash {
		t.Errorf("drifted = %+v; want query drift and differing hashes", drifted)
	}

	untracked := decode(drift.Comparison{Name: "u", State: drift.Untracked, Deployed: desc("q", 2, false)})
	if untracked.Verdict.Drift != "untracked" || untracked.Verdict.Owner != "unknown" || untracked.Right.Hash != "" || untracked.Left.Hash == "" || untracked.Changes != nil {
		t.Errorf("untracked = %+v (verdict %+v); want deployed hash only, owner unknown", untracked, untracked.Verdict)
	}

	// Metadata-less but deployed: lastDeployed (event time) is present, lastWrite isn't.
	adhoc := decode(drift.Comparison{Name: "a", State: drift.Untracked, Deployed: desc("q", 2, false), DeployedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)})
	if adhoc.Verdict.LastDeployed == "" || adhoc.Verdict.LastWrite != nil {
		t.Errorf("adhoc = %+v; want deploy time + no last-write", adhoc.Verdict)
	}

	// An orphan (untracked, gaffer's tool entry) carries owner + deploy time + the tool.
	orphan := decode(drift.Comparison{Name: "o", State: drift.Untracked, Deployed: desc("q", 2, false), Ledger: ledgerEntry(remote.ToolName, "admin")})
	if orphan.Verdict.Owner != "orphan" || orphan.Verdict.LastDeployed == "" || orphan.Verdict.LastWrite == nil || orphan.Verdict.LastWrite.Tool != remote.ToolName || orphan.Verdict.LastWrite.Actor != "admin" {
		t.Errorf("orphan = %+v; want owner orphan + deploy time + gaffer last-write", orphan.Verdict)
	}

	// A drifted projection still matching my last deploy attributes to a local edit.
	localAhead := decode(drift.Comparison{
		Name: "la", State: drift.Drifted, Cmp: deploy.Comparison{QueryDiffers: true},
		Local: desc("x", 2, false), Deployed: desc("y", 2, false),
		Ledger: ledgerEntry(remote.ToolName, "admin"), DeployBaseline: desc("y", 2, false),
	})
	if localAhead.Verdict.Attribution != "local-ahead" || localAhead.Verdict.Owner != "in-config" || localAhead.Verdict.LastWrite == nil {
		t.Errorf("local-ahead = %+v; want attribution local-ahead, owner in-config", localAhead.Verdict)
	}

	// Invalid: report the compile error and the deployed hash, but no local hash
	// (emit can't be derived) and no changes verdict.
	invalid := decode(drift.Comparison{
		Name: "i", State: drift.Invalid,
		Local: desc("q", 2, false), Deployed: desc("q", 2, false),
		LocalErr: errors.New("boom"),
	})
	if invalid.Verdict.Drift != "invalid" || invalid.Verdict.Reason != "boom" || invalid.Right.Hash != "" || invalid.Left.Hash == "" || invalid.Changes != nil {
		t.Errorf("invalid = %+v (verdict %+v); want error + deployed hash only", invalid, invalid.Verdict)
	}
}
