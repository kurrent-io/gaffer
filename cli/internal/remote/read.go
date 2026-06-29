package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
)

const (
	// projectionStreamPrefix + name is the system stream holding a projection's
	// persisted state, one $ProjectionUpdated event per create/update.
	projectionStreamPrefix = "$projections-"
	projectionUpdatedType  = "$ProjectionUpdated"

	// definitionScanLimit bounds the backward read for the latest definition.
	// The stream holds only $ProjectionUpdated events, so the newest one is the
	// current definition; a small batch (rather than a single event) lets the
	// reader skip past any unexpected trailing event type without a re-read.
	definitionScanLimit = 10
)

// Definition is a projection's deployed definition, recovered from its persisted
// state. It is what deploy and diff compare local source against; gaffer's own
// shape, decoded from the server's PersistedState DTO.
type Definition struct {
	Query               string
	EngineVersion       int    // 1 or 2; absent on the wire means 1
	Mode                string // e.g. Continuous
	Emit                bool   // emitEnabled; absent means false
	TrackEmittedStreams bool   // absent means false
	Enabled             bool
	// Time is when this definition was written: the $ProjectionUpdated event's
	// CreatedDate. It's event metadata, not part of the persisted-state DTO, so it's
	// available even for a projection carrying no tool metadata - the last-deploy/write
	// time status shows when there's no ledger to read it from.
	Time time.Time
}

// persistedState is the subset of the server's PersistedState DTO gaffer reads
// back. The server serialises it camelCase (CamelCasePropertyNamesContractResolver)
// with enums as strings and null/default fields omitted; Go's case-insensitive
// unmarshalling tolerates a casing change, and the pointer/zero defaults below
// map an omitted field to its documented meaning.
//
// handlerType (the projection's handler kind) is intentionally not decoded:
// gaffer projections are JavaScript-only today, so it's a constant. Add it when
// diff needs to guard that a deployed projection is the same kind before
// comparing query text.
type persistedState struct {
	Query               string `json:"query"`
	EngineVersion       int    `json:"engineVersion"`
	Mode                string `json:"mode"`
	EmitEnabled         *bool  `json:"emitEnabled"`
	TrackEmittedStreams *bool  `json:"trackEmittedStreams"`
	Enabled             bool   `json:"enabled"`
	Deleted             bool   `json:"deleted"`
	Deleting            bool   `json:"deleting"`
}

// Read returns the deployed definition of a projection, or ErrNotFound if it is
// not deployed (no persisted-state stream, or its last state is a tombstone). It
// reads the last $ProjectionUpdated event from the projection's
// $projections-<name> system stream, which requires admin/$ops read access.
func (c *Client) Read(ctx context.Context, name string) (*Definition, error) {
	stream, err := c.db.ReadStream(ctx, projectionStreamPrefix+name, kurrentdb.ReadStreamOptions{
		Direction: kurrentdb.Backwards,
		From:      kurrentdb.End{},
		// Read from the leader for the same reason the management and statistics
		// calls do: the leader holds the current definition, so a diff or deploy
		// comparison is not racing a lagging follower.
		RequiresLeader: true,
	}, definitionScanLimit)
	if err != nil {
		return nil, classify(err)
	}
	defer stream.Close()
	return readDefinition(stream.Recv, name)
}

// scanLatest walks a backwards event stream newest-first for the first event
// of wantType, decodes it with parse, and returns (value, true, nil). It
// returns (zero, false, nil) when the stream exhausts (io.EOF) without a
// match, and (zero, false, classify(err)) on any other read error. Callers
// own the not-found policy: map !found to a sentinel, or to a nil result.
//
// next is the stream's Recv. Split out so the loop is exercised in tests
// without a live read stream, and shared by Read and ServerInfo, which
// differ only in their terminal cases.
func scanLatest[T any](next func() (*kurrentdb.ResolvedEvent, error), wantType string, parse func(*kurrentdb.RecordedEvent) (T, error)) (T, bool, error) {
	var zero T
	for {
		ev, err := next()
		if errors.Is(err, io.EOF) {
			return zero, false, nil
		}
		if err != nil {
			return zero, false, classify(err)
		}
		// A non-error Recv yields a non-nil event in practice; guard ev anyway so
		// an injected next that returns (nil, nil) skips rather than panics. ev.Event
		// is nil for a resolved link with no underlying event.
		if ev == nil || ev.Event == nil || ev.Event.EventType != wantType {
			continue
		}
		v, err := parse(ev.Event)
		if err != nil {
			return zero, false, err
		}
		return v, true, nil
	}
}

// readDefinition walks events newest-first until it finds a $ProjectionUpdated,
// decodes it, and returns the definition. A missing stream surfaces as an error
// from next (classified to ErrNotFound); exhausting the stream without a state
// event, or a tombstoned state, is ErrNotFound. Split from Read so the loop is
// testable without a live read stream.
func readDefinition(next func() (*kurrentdb.ResolvedEvent, error), name string) (*Definition, error) {
	def, found, err := scanLatest(next, projectionUpdatedType, func(e *kurrentdb.RecordedEvent) (*Definition, error) {
		d, err := parseDefinition(e.Data, name)
		if err != nil {
			return nil, err
		}
		d.Time = e.CreatedDate
		return d, nil
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return def, nil
}

func parseDefinition(data []byte, name string) (*Definition, error) {
	var ps persistedState
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, fmt.Errorf("decode persisted state for %q: %w", name, err)
	}
	// A deleted or in-flight-deleting projection still has a persisted-state
	// stream; treat it as absent so callers do not diff or update a tombstone.
	if ps.Deleted || ps.Deleting {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	engineVersion := ps.EngineVersion
	if engineVersion == 0 {
		engineVersion = 1
	}
	return &Definition{
		Query:               ps.Query,
		EngineVersion:       engineVersion,
		Mode:                ps.Mode,
		Emit:                ps.EmitEnabled != nil && *ps.EmitEnabled,
		TrackEmittedStreams: ps.TrackEmittedStreams != nil && *ps.TrackEmittedStreams,
		Enabled:             ps.Enabled,
	}, nil
}
