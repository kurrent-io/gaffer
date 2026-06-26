package engine

import (
	"fmt"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
)

// CompileResult is the output of the compile preamble: the resolved
// Projection plus the live runtime session and its populated
// ProjectionInfo. The caller owns the Session and must Destroy it.
type CompileResult struct {
	Projection *Projection
	Session    *gafferruntime.Session
	Info       gafferruntime.ProjectionInfo
}

// ProjectionNotFoundError reports that no projection with the given name
// is declared in gaffer.toml. Returned by CompileNamed so callers can
// distinguish a typo/missing entry from a config or compile failure and
// shape their own message (e.g. point at list_projections).
type ProjectionNotFoundError struct {
	Name string
}

func (e ProjectionNotFoundError) Error() string {
	return fmt.Sprintf("projection %q not found in gaffer.toml", e.Name)
}

// ProjectionConfigError reports a per-projection config-validation failure
// (the error config.ProjectionConfigError defers past Load). Returned by
// CompileNamed wrapped so callers can tell a static config problem apart
// from a runtime compile failure (gafferruntime.ProjectionError) - the two
// are shaped differently by some surfaces (info degrades, validate reports
// valid:false with the raw reason).
type ProjectionConfigError struct {
	Name string
	Err  error
}

func (e ProjectionConfigError) Error() string { return e.Err.Error() }
func (e ProjectionConfigError) Unwrap() error { return e.Err }

// SourceReadError reports a failure reading the projection's entry file
// (ReadSource). Wrapped so callers can tell a missing/unreadable source
// file apart from a runtime compile failure, which some surfaces phrase
// differently ("creating session: ...").
type SourceReadError struct {
	Err error
}

func (e SourceReadError) Error() string { return e.Err.Error() }
func (e SourceReadError) Unwrap() error { return e.Err }

// CompileNamed runs the projection compile preamble shared by every
// CLI and MCP surface that loads a projection by name: find it in the
// config, surface a per-projection config error, read its source, build
// the Projection, and create a live runtime session.
//
// The phase that fails is reported via a typed error - ProjectionNotFoundError,
// ProjectionConfigError, the wrapped ReadSource error, or the runtime's
// gafferruntime.ProjectionError from CreateSession - so callers keep only
// their own result shaping (toolError vs styled stderr vs degraded info)
// and recording (projection_errors_seen on a gafferruntime.ProjectionError).
// On success the caller owns CompileResult.Session and must Destroy it.
//
// debug/includeShape are passed straight to CreateSession (see its doc for
// includeShape's walker-cost gate).
func CompileNamed(cfg *config.Config, root, name string, debug, includeShape bool) (*CompileResult, error) {
	def := cfg.FindProjection(name)
	if def == nil {
		return nil, ProjectionNotFoundError{Name: name}
	}
	// Per-projection config errors are deferred past config.Load so one bad
	// projection doesn't block the others; check the one being compiled here
	// rather than compiling past it and wrongly reporting valid.
	if cfgErr := cfg.ProjectionConfigError(name); cfgErr != nil {
		return nil, ProjectionConfigError{Name: name, Err: cfgErr}
	}

	source, err := ReadSource(root, def.Entry)
	if err != nil {
		return nil, SourceReadError{Err: err}
	}

	proj := NewProjection(root, cfg, def, source)
	session, info, err := CreateSession(proj, debug, includeShape)
	if err != nil {
		return nil, err
	}

	return &CompileResult{Projection: proj, Session: session, Info: info}, nil
}
