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

// confirmPlan gates the apply phase on confirmation when the plan would change
// something (create or update; counts passed in). Returns nil to proceed,
// prompt.ErrCancelled if the user declines (a clean abort), or errNeedConfirm
// when it can't ask (non-interactive or --json) and --yes wasn't given. A plan
// that only skips or refuses changes nothing, so it proceeds without asking.
func confirmPlan(out, errOut io.Writer, plan []plannedItem, target string, creates, updates int, yes, jsonOut, prod bool) error {
	if creates+updates == 0 {
		return nil
	}
	// --yes is an explicit confirmation, so it skips the prompt everywhere,
	// production included; prod only makes the prompt louder when it does fire.
	// (The blanket bypass that production refuses is --force, handled before this.)
	if !jsonOut && prompt.Enabled(yes) {
		newTextWriter(out, out).writePlanSummary(plan, target, creates, updates, prod)
		ok, err := prompt.Confirm(confirmTitle(creates+updates, target, prod), false)
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
		// surface faulted update targets - the --yes/CI path is where an operator
		// is least watching, and clobbering a faulted projection is worth a note.
		newTextWriter(errOut, errOut).writeFaultedWarnings(plan)
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

// planChangeCounts tallies the projections the plan would create and update -
// the changes a confirm is about. Skips, refusals and planning errors change
// nothing and aren't counted.
func planChangeCounts(plan []plannedItem) (creates, updates int) {
	for _, it := range plan {
		if it.err != nil {
			continue
		}
		switch it.action {
		case actCreate:
			creates++
		case actUpdate:
			updates++
		}
	}
	return creates, updates
}

// confirmTitle is the yes/no question: the change count and, when known, the
// target it lands on. A production target is named as such so the prompt reads
// louder.
func confirmTitle(n int, target string, prod bool) string {
	noun := "change"
	if n != 1 {
		noun = "changes"
	}
	where := target
	if prod {
		if where == "" {
			where = "production"
		} else {
			where = "production " + where
		}
	}
	if where != "" {
		return fmt.Sprintf("Apply %d %s to %s?", n, noun, where)
	}
	return fmt.Sprintf("Apply %d %s?", n, noun)
}

// faultedUpdates names the update targets currently faulted on the server, so
// the confirm can warn that updating won't clear the fault.
func faultedUpdates(plan []plannedItem) []string {
	var names []string
	for _, it := range plan {
		if it.err == nil && it.action == actUpdate && it.faulted {
			names = append(names, it.name)
		}
	}
	return names
}
