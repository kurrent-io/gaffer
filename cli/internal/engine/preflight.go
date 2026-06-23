package engine

import (
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
)

// Preflight compiles a projection without running it, reporting whether it is
// safe to deploy. It returns an error if the source fails to compile at all
// (parse error, invalid projection), and otherwise the error-severity
// diagnostics the runtime flagged on a source that did compile - known to fault
// on the server, such as a quirk that reproduces an upstream engine crash. A
// deployable projection returns nil, nil.
//
// It is a distinct gate from LocalDescriptor: that derives the deployable shape
// and only cares whether the session constructs, whereas preflight also rejects
// the error diagnostics a constructed session can still carry.
func Preflight(proj *Projection) ([]gafferruntime.Diagnostic, error) {
	session, info, err := CreateSession(proj, false, false)
	if err != nil {
		return nil, err
	}
	defer session.Destroy()

	return ErrorDiagnostics(info.Diagnostics), nil
}

// ErrorDiagnostics returns the error-severity diagnostics from a compiled
// projection's set - the ones known to fault on (or be rejected by) the server,
// such as a V2-incompatible feature. deploy/recreate preflight refuse on these,
// and validate reports the projection invalid for them.
func ErrorDiagnostics(diags []gafferruntime.Diagnostic) []gafferruntime.Diagnostic {
	var errs []gafferruntime.Diagnostic
	for _, d := range diags {
		if d.Severity == gafferruntime.DiagnosticSeverityError {
			errs = append(errs, d)
		}
	}
	return errs
}
