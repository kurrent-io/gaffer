package engine

import (
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
)

// LocalDescriptor builds the deployable descriptor of a local projection, for
// comparison against what's deployed (gaffer diff / status / deploy). The query
// is the raw source (projections are deployed verbatim), engine version and
// track-emitted-streams come from config, and emit is derived by compiling the
// projection. It lives here, not in deploy, because deriving emit needs the
// runtime - keeping deploy a pure, cgo-free leaf.
//
// It compiles the projection, so it fails on a source that doesn't compile.
func LocalDescriptor(proj *Projection) (deploy.Descriptor, error) {
	emit, err := derivesEmit(proj)
	if err != nil {
		return deploy.Descriptor{}, err
	}
	return deploy.Descriptor{
		Query:               proj.Source,
		EngineVersion:       proj.EngineVersion,
		Emit:                emit,
		TrackEmittedStreams: proj.Def.TrackEmittedStreams != nil && *proj.Def.TrackEmittedStreams,
	}, nil
}

// derivesEmit compiles the projection with shape analysis on and reports whether
// it writes events.
func derivesEmit(proj *Projection) (bool, error) {
	session, info, err := CreateSession(proj, false, true)
	if err != nil {
		return false, err
	}
	defer session.Destroy()
	return shapeEmits(info.Shape), nil
}

// shapeEmits reports whether a projection's shape shows it writing events:
// emit(), linkTo(), linkStreamTo(), or copyTo(). All four are in-handler write
// sinks, so the server needs EmitEnabled for any of them.
func shapeEmits(shape *gafferruntime.ProjectionShape) bool {
	if shape == nil {
		return false
	}
	c := shape.BuiltinCounts
	return positive(c.Emit) || positive(c.LinkTo) || positive(c.LinkStreamTo) || positive(c.CopyTo)
}

func positive(p *int) bool { return p != nil && *p > 0 }
