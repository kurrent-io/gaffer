package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
)

// preflightFailure is one projection that can't be deployed: either it failed to
// compile (CompileErr set) or it compiled but carries error-severity diagnostics
// (known to fault on the server). The two are mutually exclusive.
type preflightFailure struct {
	Name        string
	CompileErr  error
	Diagnostics []gafferruntime.Diagnostic
}

// reasons returns one human line per problem: the compile error, or each
// error diagnostic as "code: message".
func (f preflightFailure) reasons() []string {
	if f.CompileErr != nil {
		return []string{f.CompileErr.Error()}
	}
	lines := make([]string, len(f.Diagnostics))
	for i, d := range f.Diagnostics {
		lines[i] = d.Code + ": " + d.Message
	}
	return lines
}

// runPreflight compiles every projection before any server write, returning the
// ones that can't be deployed. Compiling up front makes a bad projection refuse
// the whole run rather than leaving a half-applied set, the way a per-projection
// compile-then-apply loop would. It stops early if the context is cancelled (an
// interrupt) - a large set of compiles shouldn't outlast a Ctrl-C; the caller
// sees the cancellation via ctx.Err() and reports it instead of the failures.
func runPreflight(ctx context.Context, root string, cfg *config.Config, names []string) []preflightFailure {
	var failures []preflightFailure
	for _, name := range names {
		if ctx.Err() != nil {
			break
		}
		def := cfg.FindProjection(name) // non-nil: deployNames only yields config names
		// A config-bad projection is refused per-projection in the plan (via
		// drift.Invalid); skip it here so it doesn't fail the all-or-nothing
		// preflight and abort the deploy of the good projections alongside it.
		if cfg.ProjectionConfigError(name) != nil {
			continue
		}
		source, err := engine.ReadSource(root, def.Entry)
		if err != nil {
			failures = append(failures, preflightFailure{Name: name, CompileErr: err})
			continue
		}
		diags, err := engine.Preflight(engine.NewProjection(root, cfg, def, source))
		switch {
		case err != nil:
			failures = append(failures, preflightFailure{Name: name, CompileErr: err})
		case len(diags) > 0:
			failures = append(failures, preflightFailure{Name: name, Diagnostics: diags})
		}
	}
	return failures
}

// renderPreflightFailures reports a preflight refusal: a JSON array of invalid
// projections, or a text block listing each failure and how to proceed. Used by
// recreate, whose compile gate runs before a destructive delete and so must
// refuse before building any plan - deploy folds the same failures into its plan
// instead (validatePlan). Returns the JSON encode error so a write failure
// surfaces rather than vanishing behind the non-zero exit.
func renderPreflightFailures(w io.Writer, jsonOut bool, total int, failures []preflightFailure) error {
	if jsonOut {
		out := make([]cliout.DeployJSON, len(failures))
		for i, f := range failures {
			out[i] = cliout.DeployJSON{Name: f.Name, Outcome: "invalid", Reason: strings.Join(f.reasons(), "; ")}
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	newTextWriter(w, w).writePreflightFailures(total, failures)
	return nil
}

// validatePlan runs the runtime's preflight over the plan's applying items and
// marks any that would fault on the server invalid, folding the diagnostics into
// the plan rather than aborting. The plan's own compile already catches source
// that won't build (a Compare failure is planned invalid); this adds the
// error-severity diagnostics that compile discards - a projection that builds but
// would fault. Only applying items (create/update/reset) are checked: a skip
// isn't being written, and a refusal/invalid already won't apply. Gated by the
// caller on --no-validate, whose whole purpose is to skip it.
func validatePlan(ctx context.Context, root string, cfg *config.Config, plan []drift.PlanItem) {
	idx := make(map[string]int, len(plan))
	var names []string
	for i := range plan {
		if plan[i].Err == nil && plan[i].Action.Applies() {
			names = append(names, plan[i].Name)
			idx[plan[i].Name] = i
		}
	}
	for _, f := range runPreflight(ctx, root, cfg, names) {
		i := idx[f.Name]
		plan[i].Action = drift.ActionInvalid
		plan[i].Reason = strings.Join(f.reasons(), "; ")
	}
}

// refuseInvalidPlan is the validate gate for a real apply: when the plan carries
// an invalid projection and --no-validate wasn't given, deploy refuses the whole
// run so a bad projection can't leave a half-applied set (the invariant the old
// preflight gate held, now enforced against the built plan). It reports the
// invalid projections - a JSON array of their verdicts, or a text block with how
// to proceed - and returns the exit-1 error. Returns nil when nothing is invalid,
// so the caller proceeds to confirm and apply.
func refuseInvalidPlan(out io.Writer, plan []drift.PlanItem, jsonOut bool) error {
	var invalid []drift.PlanItem
	for _, it := range plan {
		if it.Action == drift.ActionInvalid {
			invalid = append(invalid, it)
		}
	}
	if len(invalid) == 0 {
		return nil
	}
	if jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(cliout.BuildPlanJSON(invalid)); err != nil {
			return err
		}
	} else {
		newTextWriter(out, out).writeInvalidProjections(len(plan), invalid)
	}
	return silent(fmt.Errorf("deploy refused: %d of %d projections are invalid", len(invalid), len(plan)))
}
