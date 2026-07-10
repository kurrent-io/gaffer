package cliout

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
)

func TestBuildDeployJSONOutcomes(t *testing.T) {
	// A recreate refusal reads as "refused" with the recreate discriminator; an
	// invalid local reads as "invalid" with no recreate flag.
	refused := BuildDeployJSON(drift.Result{Name: "r", Action: drift.ActionRefuse, Reason: "engine version"})
	if refused.Outcome != "refused" || !refused.Recreate {
		t.Errorf("refused = %+v; want outcome refused + recreate", refused)
	}
	invalid := BuildDeployJSON(drift.Result{Name: "i", Action: drift.ActionInvalid, Reason: "won't compile"})
	if invalid.Outcome != "invalid" || invalid.Recreate {
		t.Errorf("invalid = %+v; want outcome invalid, no recreate", invalid)
	}
	// The tool behind an external change rides through to the item.
	ext := BuildDeployJSON(drift.Result{Name: "e", Action: drift.ActionUpdate, ExternalChange: true, ExternalChangeTool: "replicator"})
	if !ext.ExternalChange || ext.ExternalChangeTool != "replicator" {
		t.Errorf("ext = %+v; want externalChange + tool", ext)
	}
}

func TestBuildPlanJSONFlags(t *testing.T) {
	plan := []drift.PlanItem{
		{Name: "faulted", Action: drift.ActionUpdate, Faulted: true},
		{Name: "reset", Action: drift.ActionReset, Cmp: drift.Comparison{Local: &deploy.Descriptor{Emit: true}}},
		{Name: "quiet-reset", Action: drift.ActionReset, Cmp: drift.Comparison{Local: &deploy.Descriptor{Emit: false}}},
	}
	got := BuildPlanJSON(plan)
	if !got[0].Faulted {
		t.Error("faulted update should carry faulted")
	}
	if !got[1].EmittingReset {
		t.Error("emitting reset should carry emittingReset")
	}
	if got[2].EmittingReset {
		t.Error("a non-emitting reset must not carry emittingReset")
	}
}

func TestBuildPlanReport(t *testing.T) {
	decode := func(r PlanReportJSON) PlanReportJSON {
		t.Helper()
		b, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var out PlanReportJSON
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return out
	}

	// A plan with an applicable change and nothing blocked is deployable, and the
	// change count excludes the skip.
	deployable := decode(BuildPlanReport([]drift.PlanItem{
		{Name: "new", Action: drift.ActionCreate},
		{Name: "same", Action: drift.ActionSkip},
	}, drift.ConfigDriftResult{}))
	if deployable.Verdict != "deployable" || deployable.Changes != 1 {
		t.Errorf("deployable = %+v; want verdict deployable, 1 change", deployable)
	}

	// Any blocked item makes the whole plan blocked, even alongside changes.
	blocked := decode(BuildPlanReport([]drift.PlanItem{
		{Name: "new", Action: drift.ActionCreate},
		{Name: "bad", Action: drift.ActionInvalid, Reason: "won't compile"},
	}, drift.ConfigDriftResult{}))
	if blocked.Verdict != "blocked" || blocked.Changes != 1 {
		t.Errorf("blocked = %+v; want verdict blocked, 1 change", blocked)
	}

	// All in sync is in-sync with no changes.
	synced := decode(BuildPlanReport([]drift.PlanItem{{Name: "same", Action: drift.ActionSkip}}, drift.ConfigDriftResult{}))
	if synced.Verdict != "in-sync" || synced.Changes != 0 {
		t.Errorf("synced = %+v; want verdict in-sync, 0 changes", synced)
	}

	// Config drift and its error are mutually exclusive and never both present.
	withDrift := BuildPlanReport(nil, drift.ConfigDriftResult{Items: []drift.ConfigDrift{{Knob: "max_state_size", Server: 1, Local: 2}}})
	if len(withDrift.ConfigDrift) != 1 || withDrift.ConfigDriftError != "" {
		t.Errorf("withDrift = %+v; want one config-drift item, no error", withDrift)
	}
	withErr := BuildPlanReport(nil, drift.ConfigDriftResult{Err: errors.New("unreachable")})
	if withErr.ConfigDriftError == "" || len(withErr.ConfigDrift) != 0 {
		t.Errorf("withErr = %+v; want error, no items", withErr)
	}
}
