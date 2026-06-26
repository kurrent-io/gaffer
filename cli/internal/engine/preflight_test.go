package engine

import (
	"strings"
	"testing"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
)

func preflightProj(source string, engineVersion int) *Projection {
	cfg := &config.Config{}
	def := &config.Projection{Name: "p", Entry: "p.js", EngineVersion: new(engineVersion)}
	return NewProjection("/tmp", cfg, def, source)
}

func TestPreflightValid(t *testing.T) {
	const plain = `fromAll().when({ $init: function () { return { n: 0 }; }, $any: function (s, e) { s.n++; return s; } })`
	diags, err := Preflight(preflightProj(plain, 2))
	if err != nil {
		t.Fatalf("Preflight: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("a clean projection should have no error diagnostics, got %v", diags)
	}
}

func TestPreflightCompileError(t *testing.T) {
	// A syntax error fails to compile, so the session never constructs.
	_, err := Preflight(preflightProj(`fromAll().when({ $any: function (s, e) { return `, 2))
	if err == nil {
		t.Fatal("a source that doesn't compile should error")
	}
}

func TestPreflightErrorDiagnostic(t *testing.T) {
	// linkStreamTo with a third (metadata) argument compiles, but reproduces an
	// upstream engine crash - the runtime flags it Error severity. Preflight must
	// surface it so deploy refuses rather than shipping a projection that faults.
	const crashing = `fromAll().when({ Archived: function (s, e) { linkStreamTo('archive', e.streamId, { reason: 'x' }); return s; } })`
	diags, err := Preflight(preflightProj(crashing, 1))
	if err != nil {
		t.Fatalf("Preflight (should compile, not error): %v", err)
	}
	if len(diags) == 0 {
		t.Fatal("expected an error-severity diagnostic for the 3-arg linkStreamTo")
	}
	for _, d := range diags {
		if d.Severity != gafferruntime.DiagnosticSeverityError {
			t.Errorf("returned a non-error diagnostic: %+v", d)
		}
	}
	if !strings.Contains(diags[0].Code, "linkStreamTo") {
		t.Errorf("diagnostic code = %q, want the linkStreamTo quirk", diags[0].Code)
	}
}
