package remote

import "github.com/kurrent-io/gaffer/cli/internal/deploy"

// Descriptor reduces a deployed definition to the comparable descriptor shared
// with the local side, so diff / status / deploy compare both sides in the same
// terms. The adapter lives here (not in deploy) to keep deploy a pure leaf.
func (d Definition) Descriptor() deploy.Descriptor {
	return deploy.Descriptor{
		Query:               d.Query,
		EngineVersion:       d.EngineVersion,
		Emit:                d.Emit,
		TrackEmittedStreams: d.TrackEmittedStreams,
	}
}
