package drift

import (
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
		wantReason []string // substrings the refuse reason must contain
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
		{"invalid refuses (can't compile under --no-validate)", Comparison{State: Invalid}, ActionRefuse, []string{"local definition is invalid"}},
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
