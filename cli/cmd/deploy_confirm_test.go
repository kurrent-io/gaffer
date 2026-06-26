package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/prompt"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// changePlan has c creates and u updates (the rest skips), so the change count
// is c+u. confirm tests pass the same counts confirmPlan's caller would.
func changePlan() []plannedItem {
	return []plannedItem{{name: "a", action: actCreate}, {name: "b", action: actSkip}}
}

func noChangePlan() []plannedItem {
	return []plannedItem{{name: "a", action: actSkip}, {name: "b", action: actRefuse, reason: "x"}}
}

func confirm(plan []plannedItem, yes, jsonOut bool) error {
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
		plan []plannedItem
		want int // 0 via nil
	}{
		{"all in sync", []plannedItem{{name: "a", action: actSkip}}, 0},
		{"changes pending", []plannedItem{{name: "a", action: actCreate}, {name: "b", action: actSkip}}, 2},
		{"a refusal blocks", []plannedItem{{name: "a", action: actCreate}, {name: "b", action: actRefuse, reason: "needs recreate"}}, 1},
		{"a planning error blocks", []plannedItem{{name: "a", action: actCreate}, {name: "b", err: errors.New("read failed")}}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := renderDryRun(&buf, tc.plan, "", planChangeCounts(tc.plan), false, false)
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
	plan := []plannedItem{{name: "a", action: actCreate}, {name: "b", action: actSkip}}
	var buf bytes.Buffer
	err := renderDryRun(&buf, plan, "", planChangeCounts(plan), false, true)
	if got := exitCodeOf(err); got != 2 {
		t.Fatalf("exit code = %d, want 2", got)
	}
	// Same array-of-outcomes schema as a real deploy: each item reports its
	// would-be outcome (present here as the past-tense verdict).
	for _, want := range []string{`"name": "a"`, `"outcome": "created"`, `"name": "b"`, `"outcome": "skipped"`} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("dry-run JSON missing %q in:\n%s", want, buf.String())
		}
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
	plan := []plannedItem{{name: "orders", action: actUpdate, faulted: true}}
	var errOut bytes.Buffer
	if err := confirmPlan(io.Discard, &errOut, plan, "prod", planChangeCounts(plan), true, false, false); err != nil {
		t.Fatalf("confirmPlan(--yes): %v", err)
	}
	if !strings.Contains(errOut.String(), "orders is faulted") {
		t.Errorf("--yes path should warn about the faulted update on stderr, got:\n%s", errOut.String())
	}
}

func TestPlanChangeCounts(t *testing.T) {
	plan := []plannedItem{
		{action: actCreate},
		{action: actCreate},
		{action: actUpdate},
		{action: actReset},
		{action: actSkip},
		{action: actRefuse},
		{action: actCreate, err: errors.New("read fail")}, // planning error: not a change
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
	plan := []plannedItem{
		{name: "a", action: actUpdate, faulted: true},
		{name: "b", action: actUpdate, faulted: false},
		{name: "c", action: actCreate, faulted: true}, // create can't be faulted (not deployed)
		{name: "d", action: actUpdate, faulted: true, err: errors.New("x")},
	}
	if got := faultedUpdates(plan); strings.Join(got, ",") != "a" {
		t.Errorf("faultedUpdates = %v, want [a]", got)
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

func TestDeployTarget(t *testing.T) {
	prod := &remote.ServerInfo{Name: "orders-prod"}
	noName := &remote.ServerInfo{}
	for _, tc := range []struct {
		env  string
		info *remote.ServerInfo
		want string
	}{
		{"staging", prod, "orders-prod"}, // cluster name wins over env label
		{"staging", noName, "staging"},   // no cluster name → env label
		{"staging", nil, "staging"},      // no server info → env label
		{"", nil, ""},                    // nothing known
	} {
		if got := deployTarget(tc.env, tc.info); got != tc.want {
			t.Errorf("deployTarget(%q, %+v) = %q, want %q", tc.env, tc.info, got, tc.want)
		}
	}
}

func TestWritePlanSummary(t *testing.T) {
	plan := []plannedItem{
		{name: "a", action: actCreate},
		{name: "b", action: actUpdate, logicChange: true, faulted: true},
		{name: "e", action: actReset, cmp: comparison{Local: desc("q", 2, true)}}, // emits
		{name: "c", action: actSkip},
		{name: "d", action: actRefuse, reason: "engine version"},
		{name: "f", action: actCreate, err: errors.New("read failed")}, // couldn't plan
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
