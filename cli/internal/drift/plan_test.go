package drift

import (
	"errors"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
)

func TestPlanAction(t *testing.T) {
	drifted := func(c deploy.Comparison, local, deployed *deploy.Descriptor) Comparison {
		return Comparison{State: Drifted, Cmp: c, Local: local, Deployed: deployed}
	}
	tests := []struct {
		name       string
		in         Comparison
		wantAction Action
		wantReason []string // substrings the refuse/invalid reason must contain
	}{
		{"not deployed creates", Comparison{State: NotDeployed}, ActionCreate, nil},
		{"in sync skips", Comparison{State: InSync}, ActionSkip, nil},
		{"query drift updates", drifted(deploy.Comparison{QueryDiffers: true}, desc("a", 2, false), desc("b", 2, false)), ActionUpdate, nil},
		{"emit drift updates", drifted(deploy.Comparison{EmitDiffers: true}, desc("a", 2, true), desc("a", 2, false)), ActionUpdate, nil},
		{
			"engine version drift refuses",
			drifted(deploy.Comparison{EngineVersionDiffers: true}, desc("a", 2, false), desc("a", 1, false)),
			ActionRefuse,
			[]string{"engine version (remote 1, local 2)", "can't be changed in place"},
		},
		{
			"track emitted streams drift refuses",
			drifted(deploy.Comparison{TrackEmittedStreamsDiffers: true},
				&deploy.Descriptor{EngineVersion: 1, TrackEmittedStreams: true},
				&deploy.Descriptor{EngineVersion: 1, TrackEmittedStreams: false}),
			ActionRefuse,
			[]string{"track emitted streams (remote false, local true)"},
		},
		{
			"both create-time fields drift refuses with both",
			drifted(deploy.Comparison{EngineVersionDiffers: true, TrackEmittedStreamsDiffers: true},
				&deploy.Descriptor{EngineVersion: 1, TrackEmittedStreams: false},
				&deploy.Descriptor{EngineVersion: 2, TrackEmittedStreams: true}),
			ActionRefuse,
			[]string{"engine version (remote 2, local 1)", "track emitted streams (remote true, local false)"},
		},
		{"query and emit drift still updates", drifted(deploy.Comparison{QueryDiffers: true, EmitDiffers: true}, desc("a", 2, true), desc("b", 2, false)), ActionUpdate, nil},
		{"invalid local is invalid, not refused", Comparison{State: Invalid}, ActionInvalid, []string{"local definition is invalid"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, reason := PlanAction(tt.in)
			if action != tt.wantAction {
				t.Errorf("action = %q, want %q", action, tt.wantAction)
			}
			if len(tt.wantReason) == 0 && reason != "" {
				t.Errorf("reason = %q, want empty", reason)
			}
			for _, want := range tt.wantReason {
				if !strings.Contains(reason, want) {
					t.Errorf("reason %q missing %q", reason, want)
				}
			}
		})
	}
}

func TestIsLogicChange(t *testing.T) {
	queryDrift := Comparison{Cmp: deploy.Comparison{QueryDiffers: true}}
	emitDrift := Comparison{Cmp: deploy.Comparison{EmitDiffers: true}}
	for _, tc := range []struct {
		name   string
		action Action
		cmp    Comparison
		want   bool
	}{
		{"update with query drift", ActionUpdate, queryDrift, true},
		{"update with emit-only drift", ActionUpdate, emitDrift, false},
		{"create is never a logic change", ActionCreate, queryDrift, false},
		{"refuse is never a logic change", ActionRefuse, queryDrift, false},
	} {
		if got := isLogicChange(tc.action, tc.cmp); got != tc.want {
			t.Errorf("%s: isLogicChange = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestResolveResets(t *testing.T) {
	mk := func() []PlanItem {
		return []PlanItem{
			{Name: "logic", Action: ActionUpdate, LogicChange: true},
			{Name: "settings", Action: ActionUpdate, LogicChange: false}, // emit-only update
			{Name: "new", Action: ActionCreate},
			{Name: "refused", Action: ActionRefuse},
		}
	}

	off := mk()
	ResolveResets(off, false)
	if off[0].Action != ActionUpdate {
		t.Errorf("flag off: logic-change should stay update, got %s", off[0].Action)
	}

	on := mk()
	ResolveResets(on, true)
	if on[0].Action != ActionReset {
		t.Errorf("flag on: logic-change should become reset, got %s", on[0].Action)
	}
	if on[1].Action != ActionUpdate {
		t.Errorf("flag on: settings-only update must NOT become reset, got %s", on[1].Action)
	}
	if on[2].Action != ActionCreate || on[3].Action != ActionRefuse {
		t.Error("flag on: create and refuse should be untouched")
	}
}

func TestResultOutcome(t *testing.T) {
	for action, want := range map[Action]string{
		ActionCreate:  "created",
		ActionUpdate:  "updated",
		ActionReset:   "rebuilt",
		ActionSkip:    "skipped",
		ActionRefuse:  "refused",
		ActionInvalid: "invalid",
	} {
		if got := (Result{Action: action}).Outcome(); got != want {
			t.Errorf("%s -> %q, want %q", action, got, want)
		}
	}
	// A failed apply reads as "failed" whatever action was attempted.
	if got := (Result{Action: ActionCreate, Err: errors.New("boom")}).Outcome(); got != "failed" {
		t.Errorf("err -> %q, want failed", got)
	}
}

func TestResultExternalChangeTool(t *testing.T) {
	item := func(action Action, c Comparison) Result { return PlanItem{Name: "p", Action: action, Cmp: c}.Result() }

	// A drifted update whose deployed def still matches another tool's last
	// deploy: changed-by-tool, so the result names that tool.
	byTool := item(ActionUpdate, Comparison{
		State: Drifted, Cmp: deploy.Comparison{QueryDiffers: true},
		Local: desc("y", 2, false), Deployed: desc("x", 2, false),
		DeployBaseline: desc("x", 2, false), Ledger: ledgerEntry("other-tool", "bob"),
	})
	if !byTool.ExternalChange || byTool.ExternalChangeTool != "other-tool" {
		t.Errorf("changed-by-tool: external=%v tool=%q; want true/other-tool", byTool.ExternalChange, byTool.ExternalChangeTool)
	}

	// Deployed diverged from the latest tool entry: changed on the server
	// directly, so it's external but there's no tool to name.
	direct := item(ActionUpdate, Comparison{
		State: Drifted, Cmp: deploy.Comparison{QueryDiffers: true},
		Local: desc("y", 2, false), Deployed: desc("x", 2, false),
		DeployBaseline: desc("z", 2, false), Ledger: ledgerEntry("other-tool", "bob"),
	})
	if !direct.ExternalChange || direct.ExternalChangeTool != "" {
		t.Errorf("changed-server: external=%v tool=%q; want true/empty", direct.ExternalChange, direct.ExternalChangeTool)
	}

	// A recreate refusal applies nothing, so it never claims an external
	// overwrite even when the server drifted.
	refused := item(ActionRefuse, Comparison{
		State: Drifted, Cmp: deploy.Comparison{EngineVersionDiffers: true},
		Local: desc("x", 3, false), Deployed: desc("x", 2, false),
		DeployBaseline: desc("z", 2, false), Ledger: ledgerEntry("other-tool", "bob"),
	})
	if refused.ExternalChange {
		t.Error("refused item must not claim an external overwrite")
	}
}
