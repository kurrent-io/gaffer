package engine

import (
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
)

// LocalDescriptor builds the deployable descriptor of a local projection, for
// comparison against what's deployed (gaffer diff / status / deploy). The query
// is the raw source (projections are deployed verbatim), engine version and
// track-emitted-streams come from config, and emit is read from the runtime's
// first-class EmitsEvents signal. It lives here, not in deploy, because
// compiling the projection needs the runtime - keeping deploy a pure, cgo-free
// leaf.
//
// It compiles the projection, so it fails on a source that doesn't compile.
func LocalDescriptor(proj *Projection) (deploy.Descriptor, error) {
	session, info, err := CreateSession(proj, false, false)
	if err != nil {
		return deploy.Descriptor{}, err
	}
	defer session.Destroy()
	return deploy.Descriptor{
		Query:               proj.Source,
		EngineVersion:       proj.EngineVersion,
		Emit:                info.EmitsEvents,
		TrackEmittedStreams: proj.Def.TrackEmittedStreams != nil && *proj.Def.TrackEmittedStreams,
	}, nil
}

// PartialDescriptor builds the descriptor WITHOUT compiling: query, engine
// version and track-emitted-streams come straight from source and config. Emit
// is left zero because it can only be derived by compiling, so the caller (a
// projection that failed to compile) must treat emit as unknown rather than
// false. For showing what a diff can without a working compile.
func PartialDescriptor(proj *Projection) deploy.Descriptor {
	return deploy.Descriptor{
		Query:               proj.Source,
		EngineVersion:       proj.EngineVersion,
		TrackEmittedStreams: proj.Def.TrackEmittedStreams != nil && *proj.Def.TrackEmittedStreams,
	}
}
