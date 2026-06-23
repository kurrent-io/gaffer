package cmd

import (
	"errors"
	"fmt"
	"io"

	"github.com/kurrent-io/gaffer/cli/internal/prompt"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// errNeedConfirm is returned when a deploy that would change something can't be
// confirmed - no terminal (or --json output) and no --yes. Fail-closed: deploy
// never applies an unconfirmed change.
var errNeedConfirm = errors.New("deploy would change projections but can't confirm: run in a terminal, or pass --yes to apply non-interactively")

// planTotals counts the changes a plan would apply, by kind. changes() is the
// total that needs confirmation; skips, refusals and planning errors aren't
// changes and aren't counted.
type planTotals struct {
	creates, updates, rebuilds int
}

func (t planTotals) changes() int { return t.creates + t.updates + t.rebuilds }

// confirmPlan gates the apply phase on confirmation when the plan would change
// something. Returns nil to proceed, prompt.ErrCancelled if the user declines (a
// clean abort), or errNeedConfirm when it can't ask (non-interactive or --json)
// and --yes wasn't given. A plan that only skips or refuses changes nothing, so
// it proceeds without asking.
func confirmPlan(out, errOut io.Writer, plan []plannedItem, target string, totals planTotals, yes, jsonOut, prod bool) error {
	if totals.changes() == 0 {
		return nil
	}
	// --yes is an explicit confirmation, so it skips the prompt everywhere,
	// production included; prod only makes the prompt louder when it does fire.
	// (The bypass that production refuses is --no-validate, handled before this.)
	if !jsonOut && prompt.Enabled(yes) {
		newTextWriter(out, out).writePlanSummary(plan, target, totals, prod)
		ok, err := prompt.Confirm(confirmTitle(totals.changes(), target, prod), false)
		if err != nil {
			return err
		}
		if !ok {
			return prompt.ErrCancelled
		}
		return nil
	}
	if yes {
		// Proceeding non-interactively: the plan summary isn't shown, so still
		// surface the per-projection cautions - the --yes/CI path is where an
		// operator is least watching.
		newTextWriter(errOut, errOut).writeApplyWarnings(plan)
		return nil
	}
	return errNeedConfirm
}

// deployTarget names the deploy target for the confirm: the server's
// self-reported cluster name when it has one (authoritative), else the env name
// the user selected, else empty.
func deployTarget(env string, info *remote.ServerInfo) string {
	if info != nil && info.Name != "" {
		return info.Name
	}
	return env
}

// targetDesc names the target for an error message: "cluster <name>" when known,
// else a generic phrase so the sentence reads regardless.
func targetDesc(target string) string {
	if target != "" {
		return "cluster " + target
	}
	return "the target server"
}

// planChangeCounts tallies the changes the plan would apply, by kind. Skips,
// refusals and planning errors change nothing and aren't counted.
func planChangeCounts(plan []plannedItem) planTotals {
	var t planTotals
	for _, it := range plan {
		if it.err != nil {
			continue
		}
		switch it.action {
		case actCreate:
			t.creates++
		case actUpdate:
			t.updates++
		case actReset:
			t.rebuilds++
		}
	}
	return t
}

// prodWhere names the target for a confirm prompt: the target with "production"
// prepended when prod, "production" alone when prod with no known target, or just
// the target otherwise. Empty when nothing is known and not production. Shared by
// the deploy and operate confirms so the production phrasing stays consistent.
func prodWhere(target string, prod bool) string {
	if !prod {
		return target
	}
	if target == "" {
		return "production"
	}
	return "production " + target
}

// confirmTitle is the yes/no question: the change count and, when known, the
// target it lands on. A production target is named as such so the prompt reads
// louder.
func confirmTitle(n int, target string, prod bool) string {
	noun := "change"
	if n != 1 {
		noun = "changes"
	}
	if where := prodWhere(target, prod); where != "" {
		return fmt.Sprintf("Apply %d %s to %s?", n, noun, where)
	}
	return fmt.Sprintf("Apply %d %s?", n, noun)
}

// faultedUpdates names the update targets currently faulted on the server, so
// the confirm can warn that updating won't clear the fault. A reset (rebuild) of
// a faulted projection isn't flagged - rebuilding from zero does clear it.
func faultedUpdates(plan []plannedItem) []string {
	var names []string
	for _, it := range plan {
		if it.err == nil && it.action == actUpdate && it.faulted {
			names = append(names, it.name)
		}
	}
	return names
}

// emittingResets names the reset targets that emit, so the confirm can warn that
// reprocessing re-emits (duplicating into the target streams), since reset can't
// clean emitted streams - gaffer recreate --delete-emitted can.
func emittingResets(plan []plannedItem) []string {
	var names []string
	for _, it := range plan {
		if it.err == nil && it.action == actReset && it.cmp.Local != nil && it.cmp.Local.Emit {
			names = append(names, it.name)
		}
	}
	return names
}
