package remote

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
)

// historyHardCap bounds a single ReadHistory call when the caller asks for "all"
// (count <= 0). A projection's $projections-<name> stream grows one event per
// create/update/lifecycle write, so an unbounded read of a long-lived projection
// could pull thousands of events; the cap keeps a degenerate stream from hanging
// the command. The interactive picker pages instead of asking for all at once.
const historyHardCap = 1000

// Version is one entry in a projection's version history: a single
// $ProjectionUpdated event on $projections-<name>. Definition is the deployed
// definition at that version (its Time is the event's write time); Ledger is the
// tool metadata stamped on it, nil for a lifecycle or external write that carried
// none. Deleted marks a tombstone version (a delete, e.g. the first half of a
// recreate) so history can show it rather than read past it. MetaErr is set when
// this version's tool metadata wouldn't decode: history is an audit log, so one
// corrupt entry flags that single version rather than blanking the whole timeline.
type Version struct {
	Number     int64
	Definition *Definition
	Deleted    bool
	Ledger     *Ledger
	MetaErr    error
}

// ReadHistory reads a window of a projection's version history, newest-first. It
// reads $projections-<name> backwards, decoding every $ProjectionUpdated event
// into a Version (definition + optional tool metadata), which requires admin/$ops
// read access.
//
// before bounds the read to versions strictly older than that revision, for
// paging the interactive picker; pass a negative before to start from the current
// head. count caps how many versions to return (count <= 0 uses historyHardCap).
//
// total is the stream's version count (head revision + 1) when reading from the
// head (before < 0); on a paged read (before >= 0) total is -1, since the head
// isn't in view and the caller already knows it from the first page.
//
// ErrNotFound when the projection's stream is absent (a backward read surfaces
// that at the first Recv), kept consistent with Read. A tombstone is a Version
// with Deleted set, not an error - a delete is an audit event in its own right.
func (c *Client) ReadHistory(ctx context.Context, name string, before int64, count int) ([]Version, int64, error) {
	if before == 0 {
		// Nothing is older than the first version, so a page below it is empty. -1
		// total: a paged read never reports the count (the caller has it already).
		return nil, -1, nil
	}
	from := kurrentdb.StreamPosition(kurrentdb.End{})
	if before > 0 {
		// Backwards from the event just below the last one already loaded, so the
		// page is strictly older than `before` with no overlap.
		from = kurrentdb.StreamPosition(kurrentdb.Revision(uint64(before - 1))) //nolint:gosec // before > 0 here, so before-1 is non-negative
	}
	limit := count
	if limit <= 0 || limit > historyHardCap {
		limit = historyHardCap
	}
	stream, err := c.db.ReadStream(ctx, projectionStreamPrefix+name, kurrentdb.ReadStreamOptions{
		Direction:      kurrentdb.Backwards,
		From:           from,
		RequiresLeader: true,
	}, uint64(limit))
	if err != nil {
		return nil, 0, classify(err)
	}
	defer stream.Close()
	return readHistory(stream.Recv, name, before < 0, limit)
}

// readHistory walks events newest-first, decoding each $ProjectionUpdated into a
// Version, up to limit. headRead reports whether this read started at the stream
// head, in which case the first event's revision + 1 is the total version count
// (else total is -1). A missing stream surfaces as an error from next (classified
// to ErrNotFound). Split from ReadHistory so the loop is testable without a live
// read stream.
func readHistory(next func() (*kurrentdb.ResolvedEvent, error), name string, headRead bool, limit int) ([]Version, int64, error) {
	versions := make([]Version, 0, limit)
	total := int64(-1)
	for len(versions) < limit {
		ev, err := next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, 0, classify(err)
		}
		if ev == nil || ev.Event == nil || ev.Event.EventType != projectionUpdatedType {
			continue
		}
		e := ev.Event
		if headRead && total < 0 {
			// The first event read from End{} is the stream head, so its revision is
			// the highest; +1 is the count of versions in the stream.
			total = int64(e.EventNumber) + 1 //nolint:gosec // event numbers are small, no overflow
		}
		v, err := parseVersion(e, name)
		if err != nil {
			return nil, 0, err
		}
		versions = append(versions, v)
	}
	return versions, total, nil
}

// parseVersion decodes one $ProjectionUpdated event into a Version. Unlike
// parseDefinition (which gates a tombstone to ErrNotFound so callers don't diff a
// deleted projection), history keeps a tombstone as a Deleted Version - a delete
// is a real point in the audit trail. Malformed tool metadata is recorded on the
// version (MetaErr) rather than aborting the read: a forensic timeline shows the
// one bad entry and keeps the rest. A persisted-state that won't decode is still
// fatal - without it there's no definition to place on the timeline at all.
func parseVersion(e *kurrentdb.RecordedEvent, name string) (Version, error) {
	def, deleted, err := parseDefinitionState(e.Data, name)
	if err != nil {
		return Version{}, err
	}
	def.Time = e.CreatedDate
	v := Version{
		Number:     int64(e.EventNumber), //nolint:gosec // event numbers are small, no overflow
		Definition: def,
		Deleted:    deleted,
	}
	l, err := parseLedger(e.UserMetadata, e.CreatedDate)
	if err != nil {
		v.MetaErr = fmt.Errorf("%w for %q: %w", ErrMalformedLedger, name, err)
		return v, nil
	}
	v.Ledger = l
	return v, nil
}
