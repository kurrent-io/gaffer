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

// The shared projection tool-metadata convention. gaffer stamps these keys onto
// the caller properties of every deploy create/update; the server records them on
// the $ProjectionUpdated event metadata, and any tool or UI can read them back to
// attribute the projection. Keys are flat and NOT $-prefixed - the server owns the
// $ namespace for its own synthetic keys and would overwrite a colliding caller key.
const (
	ledgerKeyTool        = "tool"
	ledgerKeyToolVersion = "tool_version"
	ledgerKeyOperation   = "operation"
	ledgerKeyRevision    = "revision"
	ledgerKeyActor       = "actor"

	// ToolName is gaffer's display-cased identity in the tool key; its presence on
	// a definition is the ownership marker.
	ToolName = "Gaffer"
)

// Operation values gaffer records. The keystone always writes OpDeploy (a
// logic-change reset is still a deploy); recreate stamps OpRecreate on its
// create; rollback lands with that feature.
const (
	OpDeploy   = "deploy"
	OpRollback = "rollback"
	OpReset    = "reset"
	OpRecreate = "recreate"
)

// Ledger is one entry in the shared tool-metadata convention, stamped onto a
// definition's $ProjectionUpdated event metadata. On write, gaffer sets
// Tool=ToolName; Revision and Actor are best-effort and omitted from the wire when
// empty. On read, every field is populated from the event (Time from its
// CreatedDate, which is the write/deploy time - the convention carries no own
// timestamp).
type Ledger struct {
	Tool        string
	ToolVersion string
	Operation   string
	Revision    string
	Actor       string
	Time        time.Time // read-only: the event's write time, not serialised
}

// metadata renders the ledger as the caller-properties map the projections API
// stamps onto the event. Each field is emitted only when non-empty, so a reader
// sees only what the writer actually set and write/read are symmetric (gaffer
// always sets tool/tool_version/operation; revision/actor are best-effort).
func (l Ledger) metadata() map[string]any {
	m := map[string]any{}
	put := func(key, value string) {
		if value != "" {
			m[key] = value
		}
	}
	put(ledgerKeyTool, l.Tool)
	put(ledgerKeyToolVersion, l.ToolVersion)
	put(ledgerKeyOperation, l.Operation)
	put(ledgerKeyRevision, l.Revision)
	put(ledgerKeyActor, l.Actor)
	return m
}

// ledgerMetadata is the nil-safe bridge to the client option: no ledger means no
// caller properties (a metadata-less server ignores them either way).
func ledgerMetadata(l *Ledger) map[string]any {
	if l == nil {
		return nil
	}
	return l.metadata()
}

// ledgerScanLimit bounds the backward read for the latest tool metadata. Lifecycle
// writes (enable/disable/reset/config) append metadata-less $ProjectionUpdated
// events on top of the last deploy's, so the scan walks past them to the newest
// tool-stamped event. A projection buried under more than this many metadata-less
// events since its last tool write reads as ErrNoLedger (degrades to
// definition-only behaviour) rather than scanning unbounded.
const ledgerScanLimit = 256

// ReadLedger returns the latest tool metadata on a projection's definition. It
// reads $projections-<name> backwards and returns the first $ProjectionUpdated
// event that carries a tool key - which may be any tool's, not just gaffer's (the
// marker for ownership decisions is Tool == ToolName). Requires admin/$ops read
// access.
//
// It also returns the definition stamped on that tool entry's event - the
// definition the tool deployed, which a caller compares against the current
// deployed definition to attribute drift ("did the server change since my last
// deploy?"). nil alongside any error.
//
// Outcomes: the latest entry (+ its definition); ErrNoLedger if the projection
// exists but no tool entry is present within the scan window (untracked, or buried
// under only metadata-less lifecycle events); ErrNotFound if the projection is gone
// - either its stream is absent (a backward read surfaces that at the first Recv)
// or its latest state is a tombstone, kept consistent with Read so a caller (e.g.
// orphan detection) doesn't mistake a deleted projection for a tracked one; or
// ErrMalformedLedger if its tool metadata won't decode - surfaced distinctly, not
// swallowed as "no ledger", so a caller can flag that one projection and continue.
func (c *Client) ReadLedger(ctx context.Context, name string) (*Ledger, *Definition, error) {
	stream, err := c.db.ReadStream(ctx, projectionStreamPrefix+name, kurrentdb.ReadStreamOptions{
		Direction:      kurrentdb.Backwards,
		From:           kurrentdb.End{},
		RequiresLeader: true,
	}, ledgerScanLimit)
	if err != nil {
		return nil, nil, classify(err)
	}
	defer stream.Close()
	return readLedger(stream.Recv, name)
}

// readLedger walks events newest-first and returns the first $ProjectionUpdated
// whose metadata carries a tool key, skipping metadata-less lifecycle events.
// Exhausting the window without one is ErrNoLedger. The newest definition event
// gates the read via parseDefinition: a tombstoned projection is ErrNotFound
// (matching Read), and an undecodable state surfaces rather than being read past.
// Split from ReadLedger so the loop is testable without a live read stream.
func readLedger(next func() (*kurrentdb.ResolvedEvent, error), name string) (*Ledger, *Definition, error) {
	checkedState := false
	for {
		ev, err := next()
		if errors.Is(err, io.EOF) {
			return nil, nil, fmt.Errorf("%w: %s", ErrNoLedger, name)
		}
		if err != nil {
			return nil, nil, classify(err)
		}
		if ev == nil || ev.Event == nil || ev.Event.EventType != projectionUpdatedType {
			continue
		}
		if !checkedState {
			checkedState = true
			if _, err := parseDefinition(ev.Event.Data, name); err != nil {
				return nil, nil, err
			}
		}
		l, err := parseLedger(ev.Event.UserMetadata, ev.Event.CreatedDate)
		if err != nil {
			return nil, nil, fmt.Errorf("%w for %q: %w", ErrMalformedLedger, name, err)
		}
		if l != nil {
			// The definition on this tool-stamped event is what the entry deployed -
			// the baseline a caller hashes against the current deployed definition to
			// tell a local-only edit from a change made on the server since.
			def, err := parseDefinition(ev.Event.Data, name)
			if err != nil {
				return nil, nil, err
			}
			return l, def, nil
		}
	}
}

// parseLedger decodes one event's metadata JSON into a Ledger, or (nil, nil) when
// it isn't a tool entry: empty/absent metadata, or no tool key (a metadata-less
// lifecycle event carries only the server's synthetic $ keys). A non-empty blob
// that fails to decode is returned as an error, not silently skipped - the server
// synthesises property metadata back to JSON, so a decode failure is a real
// anomaly, not an absence. The caller's keys sit at the top level, so a plain
// object decode suffices.
func parseLedger(metadata []byte, created time.Time) (*Ledger, error) {
	if len(metadata) == 0 {
		return nil, nil
	}
	var raw map[string]any
	if err := json.Unmarshal(metadata, &raw); err != nil {
		return nil, err
	}
	tool := stringField(raw, ledgerKeyTool)
	if tool == "" {
		return nil, nil
	}
	return &Ledger{
		Tool:        tool,
		ToolVersion: stringField(raw, ledgerKeyToolVersion),
		Operation:   stringField(raw, ledgerKeyOperation),
		Revision:    stringField(raw, ledgerKeyRevision),
		Actor:       stringField(raw, ledgerKeyActor),
		Time:        created,
	}, nil
}

func stringField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}
