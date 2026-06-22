package cmd

import (
	"encoding/json"
	"io"
	"strings"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
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
// compile-then-apply loop would.
func runPreflight(root string, cfg *config.Config, names []string) []preflightFailure {
	var failures []preflightFailure
	for _, name := range names {
		def := cfg.FindProjection(name) // non-nil: deployNames only yields config names
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

// renderPreflightFailures reports the refusal: a JSON array of invalid
// projections, or a text block listing each failure and how to proceed. Kept
// separate from the deploy sink because preflight is a gate before any apply -
// its outcomes aren't apply verdicts.
func renderPreflightFailures(w io.Writer, jsonOut bool, total int, failures []preflightFailure) {
	if jsonOut {
		out := make([]deployJSON, len(failures))
		for i, f := range failures {
			out[i] = deployJSON{Name: f.Name, Outcome: "invalid", Reason: strings.Join(f.reasons(), "; ")}
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}
	newTextWriter(w, w).writePreflightFailures(total, failures)
}
