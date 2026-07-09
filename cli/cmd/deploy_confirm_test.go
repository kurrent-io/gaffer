package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/prompt"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// changePlan has c creates and u updates (the rest skips), so the change count
// is c+u. confirm tests pass the same counts confirmPlan's caller would.
func changePlan() []drift.PlanItem {
	return []drift.PlanItem{{Name: "a", Action: drift.ActionCreate}, {Name: "b", Action: drift.ActionSkip}}
}

func noChangePlan() []drift.PlanItem {
	return []drift.PlanItem{{Name: "a", Action: drift.ActionSkip}, {Name: "b", Action: drift.ActionRefuse, Reason: "x"}}
}

func confirm(plan []drift.PlanItem, yes, jsonOut bool) error {
	return confirmPlan(io.Discard, io.Discard, plan, "", planChangeCounts(plan), yes, jsonOut, false)
}

func TestConfirmPlan(t *testing.T) {
	// A plan that changes nothing proceeds without asking, whatever the flags.
	if err := confirm(noChangePlan(), false, false); err != nil {
		t.Errorf("no-change plan should proceed, got %v", err)
	}
	// --yes proceeds without prompting.
	if err := confirm(changePlan(), true, false); err != nil {
		t.Errorf("--yes should proceed, got %v", err)
	}
	// --json can't prompt: without --yes it fails closed, with --yes it proceeds.
	if err := confirm(changePlan(), false, true); !errors.Is(err, errNeedConfirm) {
		t.Errorf("--json without --yes should fail closed, got %v", err)
	}
	if err := confirm(changePlan(), true, true); err != nil {
		t.Errorf("--json --yes should proceed, got %v", err)
	}
	// Non-interactive (no TTY) without --yes fails closed. Guarded so a TTY test
	// run - where this path would prompt and block - skips rather than hangs.
	if !prompt.Enabled(false) {
		if err := confirm(changePlan(), false, false); !errors.Is(err, errNeedConfirm) {
			t.Errorf("non-interactive without --yes should fail closed, got %v", err)
		}
	}
}

func TestRenderDryRunExitCodes(t *testing.T) {
	cases := []struct {
		name string
		plan []drift.PlanItem
		want int // 0 via nil
	}{
		{"all in sync", []drift.PlanItem{{Name: "a", Action: drift.ActionSkip}}, 0},
		{"changes pending", []drift.PlanItem{{Name: "a", Action: drift.ActionCreate}, {Name: "b", Action: drift.ActionSkip}}, 2},
		{"a refusal blocks", []drift.PlanItem{{Name: "a", Action: drift.ActionCreate}, {Name: "b", Action: drift.ActionRefuse, Reason: "needs recreate"}}, 1},
		{"a planning error blocks", []drift.PlanItem{{Name: "a", Action: drift.ActionCreate}, {Name: "b", Err: errors.New("read failed")}}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := renderDryRun(&buf, tc.plan, "", "", nil, planChangeCounts(tc.plan), drift.ConfigDriftResult{}, false)
			got := 0
			if err != nil {
				got = ExitCodeFor(err)
			}
			if got != tc.want {
				t.Errorf("exit code = %d, want %d (err %v)", got, tc.want, err)
			}
			if buf.Len() == 0 {
				t.Error("dry run rendered no plan")
			}
		})
	}
}

func TestRenderDryRunJSONShape(t *testing.T) {
	plan := []drift.PlanItem{{Name: "a", Action: drift.ActionCreate}, {Name: "b", Action: drift.ActionSkip}}
	var buf bytes.Buffer
	err := renderDryRun(&buf, plan, "staging", "orders-cluster", nil, planChangeCounts(plan), drift.ConfigDriftResult{}, true)
	if got := exitCodeOf(err); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
	// The PlanReportJSON envelope: the top-level verdict and env/target, wrapping
	// the plan array whose items each report their would-be outcome.
	var got cliout.PlanReportJSON
	if uerr := json.Unmarshal(buf.Bytes(), &got); uerr != nil {
		t.Fatalf("unmarshal: %v\n%s", uerr, buf.String())
	}
	if got.Verdict != "deployable" || got.Env != "staging" || got.Target != "orders-cluster" || got.Changes != 1 {
		t.Errorf("envelope = %+v; want verdict deployable, env staging, target orders-cluster, 1 change", got)
	}
	if len(got.Plan) != 2 || got.Plan[0].Outcome != "created" || got.Plan[1].Outcome != "skipped" {
		t.Errorf("plan = %+v; want created then skipped", got.Plan)
	}
}

func TestExitCodeFor(t *testing.T) {
	if got := ExitCodeFor(errors.New("plain")); got != 1 {
		t.Errorf("plain error should be exit 1, got %d", got)
	}
	if got := ExitCodeFor(exitWith(2, silent(errors.New("pending")))); got != 2 {
		t.Errorf("exitWith(2) should be exit 2, got %d", got)
	}
	if got := ExitCodeFor(errNeedConfirm); got != 3 {
		t.Errorf("errNeedConfirm should be exit 3, got %d", got)
	}
	if got := ExitCodeFor(errOperateNeedsConfirm); got != 3 {
		t.Errorf("errOperateNeedsConfirm should be exit 3, got %d", got)
	}
	// A guardrail sentinel still maps to 3 when wrapped (fmt.Errorf %w).
	if got := ExitCodeFor(fmt.Errorf("deploy: %w", errNeedConfirm)); got != 3 {
		t.Errorf("wrapped errNeedConfirm should be exit 3, got %d", got)
	}
}

// exitCodeOf returns the mapped exit code, or 0 for a nil error.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	return ExitCodeFor(err)
}

func TestConfirmPlanYesSkipsOnProd(t *testing.T) {
	// --yes is an explicit confirmation and proceeds even against production - prod
	// only blocks the --no-validate bypass, not an explicit --yes.
	plan := changePlan()
	if err := confirmPlan(io.Discard, io.Discard, plan, "orders-prod", planChangeCounts(plan), true, false, true); err != nil {
		t.Errorf("--yes should proceed even on production, got %v", err)
	}
}

func TestConfirmPlanYesWarnsFaulted(t *testing.T) {
	plan := []drift.PlanItem{{Name: "orders", Action: drift.ActionUpdate, Faulted: true}}
	var errOut bytes.Buffer
	if err := confirmPlan(io.Discard, &errOut, plan, "prod", planChangeCounts(plan), true, false, false); err != nil {
		t.Fatalf("confirmPlan(--yes): %v", err)
	}
	if !strings.Contains(errOut.String(), "orders is faulted") {
		t.Errorf("--yes path should warn about the faulted update on stderr, got:\n%s", errOut.String())
	}
}

func TestPlanChangeCounts(t *testing.T) {
	plan := []drift.PlanItem{
		{Action: drift.ActionCreate},
		{Action: drift.ActionCreate},
		{Action: drift.ActionUpdate},
		{Action: drift.ActionReset},
		{Action: drift.ActionSkip},
		{Action: drift.ActionRefuse},
		{Action: drift.ActionCreate, Err: errors.New("read fail")}, // planning error: not a change
	}
	got := planChangeCounts(plan)
	if got.creates != 2 || got.updates != 1 || got.rebuilds != 1 {
		t.Errorf("totals = %+v, want creates 2, updates 1, rebuilds 1", got)
	}
	if got.changes() != 4 {
		t.Errorf("changes() = %d, want 4", got.changes())
	}
}

func TestFaultedUpdates(t *testing.T) {
	plan := []drift.PlanItem{
		{Name: "a", Action: drift.ActionUpdate, Faulted: true},
		{Name: "b", Action: drift.ActionUpdate, Faulted: false},
		{Name: "c", Action: drift.ActionCreate, Faulted: true}, // create can't be faulted (not deployed)
		{Name: "d", Action: drift.ActionUpdate, Faulted: true, Err: errors.New("x")},
	}
	if got := faultedUpdates(plan); strings.Join(got, ",") != "a" {
		t.Errorf("faultedUpdates = %v, want [a]", got)
	}
}

// drift comparisons for the external-change cases: all are drifted updates; what
// differs is the ledger and whether the deployed def still matches the baseline.
func changedServer() drift.Comparison { // a metadata-less/direct write changed gaffer's deploy
	return drift.Comparison{State: drift.Drifted, Ledger: ledgerEntry(remote.ToolName, ""), Deployed: desc("a", 2, false), DeployBaseline: desc("b", 2, false)}
}

func changedByTool() drift.Comparison { // another tool is the current deployer
	return drift.Comparison{State: drift.Drifted, Ledger: ledgerEntry("KurrentDB Embedded UI", ""), Deployed: desc("a", 2, false), DeployBaseline: desc("a", 2, false)}
}

func localAhead() drift.Comparison { // server still holds gaffer's last deploy - not external
	return drift.Comparison{State: drift.Drifted, Ledger: ledgerEntry(remote.ToolName, ""), Deployed: desc("a", 2, false), DeployBaseline: desc("a", 2, false)}
}

func TestExternallyChangedTargets(t *testing.T) {
	plan := []drift.PlanItem{
		{Name: "srv", Action: drift.ActionUpdate, Cmp: changedServer()},
		{Name: "tool", Action: drift.ActionUpdate, Cmp: changedByTool()},
		{Name: "ahead", Action: drift.ActionUpdate, Cmp: localAhead()},                                                             // not external
		{Name: "noledger", Action: drift.ActionUpdate, Cmp: drift.Comparison{State: drift.Drifted, Deployed: desc("a", 2, false)}}, // drift.AttrNone
		{Name: "refused", Action: drift.ActionRefuse, Cmp: changedServer()},                                                        // won't apply, so not flagged
		{Name: "errored", Err: errors.New("x"), Cmp: changedServer()},
	}
	got := externallyChangedTargets(plan)
	if len(got) != 2 {
		t.Fatalf("got %d targets, want 2: %+v", len(got), got)
	}
	if got[0].name != "srv" || got[0].tool != "" {
		t.Errorf("changed-server target = %+v; want {srv, \"\"}", got[0])
	}
	if got[1].name != "tool" || got[1].tool != "KurrentDB Embedded UI" {
		t.Errorf("changed-by-tool target = %+v; want {tool, KurrentDB Embedded UI}", got[1])
	}
}

func TestWriteApplyWarningsExternalChange(t *testing.T) {
	var b bytes.Buffer
	newTextWriter(&b, &b).writeApplyWarnings([]drift.PlanItem{
		{Name: "srv", Action: drift.ActionUpdate, Cmp: changedServer()},
		{Name: "tool", Action: drift.ActionUpdate, Cmp: changedByTool()},
		{Name: "ahead", Action: drift.ActionUpdate, Cmp: localAhead()},
	})
	out := b.String()
	if !strings.Contains(out, "srv was changed outside gaffer since its last deploy; deploying overwrites it") {
		t.Errorf("missing changed-server caution:\n%s", out)
	}
	if !strings.Contains(out, "tool was changed outside gaffer (by KurrentDB Embedded UI) since its last deploy; deploying overwrites it") {
		t.Errorf("missing changed-by-tool caution:\n%s", out)
	}
	if strings.Contains(out, "ahead") {
		t.Errorf("local-ahead should not be cautioned:\n%s", out)
	}
}

func TestDeployResultExternalChange(t *testing.T) {
	// An applied update over an external change carries the flag; local-ahead and a
	// non-applying refusal don't.
	if r := (drift.PlanItem{Name: "srv", Action: drift.ActionUpdate, Cmp: changedServer()}).Result(); !r.ExternalChange {
		t.Error("changed-server update should set ExternalChange")
	}
	if r := (drift.PlanItem{Name: "ahead", Action: drift.ActionUpdate, Cmp: localAhead()}).Result(); r.ExternalChange {
		t.Error("local-ahead update should not set ExternalChange")
	}
	if r := (drift.PlanItem{Name: "refused", Action: drift.ActionRefuse, Cmp: changedServer()}).Result(); r.ExternalChange {
		t.Error("a refusal applies nothing, so it should not set ExternalChange")
	}
}

func TestConfirmTitle(t *testing.T) {
	for _, tc := range []struct {
		n      int
		target string
		prod   bool
		want   string
	}{
		{1, "orders-prod", false, "Apply 1 change to orders-prod?"},
		{2, "staging", false, "Apply 2 changes to staging?"},
		{3, "", false, "Apply 3 changes?"},
		{1, "orders", true, "Apply 1 change to production orders?"},
		{2, "", true, "Apply 2 changes to production?"},
	} {
		if got := confirmTitle(tc.n, tc.target, tc.prod); got != tc.want {
			t.Errorf("confirmTitle(%d, %q, %v) = %q, want %q", tc.n, tc.target, tc.prod, got, tc.want)
		}
	}
}

func TestWritePlanSummary(t *testing.T) {
	plan := []drift.PlanItem{
		{Name: "a", Action: drift.ActionCreate},
		{Name: "b", Action: drift.ActionUpdate, LogicChange: true, Faulted: true},
		{Name: "e", Action: drift.ActionReset, Cmp: drift.Comparison{Local: desc("q", 2, true)}}, // emits
		{Name: "c", Action: drift.ActionSkip},
		{Name: "d", Action: drift.ActionRefuse, Reason: "engine version"},
		{Name: "f", Action: drift.ActionCreate, Err: errors.New("read failed")}, // couldn't plan
	}
	var buf bytes.Buffer
	newTextWriter(&buf, &buf).writePlanSummary(plan, "orders-prod", planChangeCounts(plan), false)
	out := buf.String()
	for _, want := range []string{
		"Plan for orders-prod:",
		// Count line.
		"1 to create", "1 to update", "1 to rebuild", "1 in sync", "1 failed", "1 refused",
		// Per-item lines: the verdict word and its dimmed detail column.
		"create",
		"update", "logic change, continuing from checkpoint",
		"rebuild", "reprocessing from zero",
		"refused", "engine version",
		"failed", "read failed",
		// Warnings still surface.
		"logic change(s) continuing from checkpoint",
		"b is faulted; updating won't clear the fault",
		"e emits; rebuilding re-emits",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plan summary missing %q in:\n%s", want, out)
		}
	}
	// In-sync projections are counted, not listed.
	if strings.Contains(out, "  c ") {
		t.Errorf("in-sync projection c should not be listed individually:\n%s", out)
	}
	// The verdict and its detail share one line (the three-column layout): the
	// per-item line carries both, the count line's "1 refused" does not.
	onOneLine := false
	for l := range strings.SplitSeq(out, "\n") {
		if strings.Contains(l, "refused") && strings.Contains(l, "engine version") {
			onOneLine = true
		}
	}
	if !onOneLine {
		t.Errorf("refused verdict and its reason should be on one line, in:\n%s", out)
	}
	if strings.Contains(out, "PRODUCTION") {
		t.Errorf("non-prod summary should not show a production banner:\n%s", out)
	}

	var prodBuf bytes.Buffer
	newTextWriter(&prodBuf, &prodBuf).writePlanSummary(plan, "orders-prod", planChangeCounts(plan), true)
	if !strings.Contains(prodBuf.String(), "PRODUCTION") {
		t.Errorf("prod summary should show a production banner:\n%s", prodBuf.String())
	}
}
