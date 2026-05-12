package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/json"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
)

// shapeCacheCap bounds Client.shapeCache so a long LSP session
// across a monorepo can't grow it unbounded. 1024 distinct
// projections per process is generous - any sane workspace
// stays well under it. Eviction is FIFO (oldest entry dropped)
// when cap is reached; the worst case is an evicted shape gets
// re-emitted on next encounter, which costs one extra envelope.
const shapeCacheCap = 1024

// EmitProjectionShape sends a projection_shape envelope for the
// projection at projectionPath. Dedupes via Client.shapeCache:
// emits on first encounter and again whenever the content hash
// drifts. No-op when ctx carries no Client (opt-out), when
// info.Shape is nil (caller didn't request shape via
// includeShape on the FFI option), or when the shape is
// unchanged since last emit for the same projection_id.
//
// projectionPath must be the absolute filesystem path to the
// projection source - the function derives the hashed
// projection_id from it via the per-install salt. Path itself
// never leaves the machine.
//
// Single-goroutine ownership is NOT required: the dedupe state
// is mutex-guarded. LSP / dev / MCP can all call this from
// request goroutines without synchronisation upstream.
func EmitProjectionShape(ctx context.Context, projectionPath string, info gafferruntime.ProjectionInfo) {
	c := ClientFromContext(ctx)
	// info.Shape == nil means the FFI didn't populate it - either
	// the caller passed includeShape:false (one-shot / non-telemetry
	// path) or telemetry was off at session-creation time. Distinct
	// from the "parse failed" signal, which arrives as
	// Shape{Parsable: false, ...} (the UnparsableShape sentinel in
	// DiagnosticCollector). Both routes short-circuit here because
	// there's nothing to emit either way.
	if c == nil || info.Shape == nil {
		return
	}
	projectionID := ProjectionID(c.identity.Salt, projectionPath)
	props := translateShape(info.Shape, projectionID)
	if !c.shapeChangedAndRecord(projectionID, shapeContentHash(props)) {
		return
	}
	c.emit(c.buildEnvelope(ProjectionShape{
		Name:       "projection_shape",
		Timestamp:  nowTimestamp(),
		Properties: props,
	}))
}

// translateShape converts the raw FFI binding shape (raw int /
// *int / int bytes) into the wire-format properties (bucketed
// RawCount / FileSizeBucket). The translation is pure: same
// input always produces the same output, so the content hash
// downstream is stable.
func translateShape(raw *gafferruntime.ProjectionShape, projectionID string) ProjectionShapeProperties {
	return ProjectionShapeProperties{
		ProjectionID: projectionID,
		Parsable:     raw.Parsable,
		FileSize:     fileSizeBucket(raw.FileSize),
		Handlers: ProjectionShapeHandlers{
			Any:                raw.Handlers.Any,
			Init:               raw.Handlers.Init,
			Deleted:            raw.Handlers.Deleted,
			DistinctEventNames: RawCount(raw.Handlers.DistinctEventNames),
		},
		BuiltinCounts: ProjectionShapeBuiltinCounts{
			FromAll:        rawCountPtr(raw.BuiltinCounts.FromAll),
			FromStream:     rawCountPtr(raw.BuiltinCounts.FromStream),
			FromStreams:    rawCountPtr(raw.BuiltinCounts.FromStreams),
			FromCategory:   rawCountPtr(raw.BuiltinCounts.FromCategory),
			FromCategories: rawCountPtr(raw.BuiltinCounts.FromCategories),
			When:           rawCountPtr(raw.BuiltinCounts.When),
			ForeachStream:  rawCountPtr(raw.BuiltinCounts.ForeachStream),
			OutputState:    rawCountPtr(raw.BuiltinCounts.OutputState),
			TransformBy:    rawCountPtr(raw.BuiltinCounts.TransformBy),
			PartitionBy:    rawCountPtr(raw.BuiltinCounts.PartitionBy),
			Emit:           rawCountPtr(raw.BuiltinCounts.Emit),
			LinkTo:         rawCountPtr(raw.BuiltinCounts.LinkTo),
			CopyTo:         rawCountPtr(raw.BuiltinCounts.CopyTo),
			LinkStreamTo:   rawCountPtr(raw.BuiltinCounts.LinkStreamTo),
			ChainHandlers:  rawCountPtr(raw.BuiltinCounts.ChainHandlers),
			UpdateOf:       rawCountPtr(raw.BuiltinCounts.UpdateOf),
		},
	}
}

// rawCountPtr converts a *int (FFI shape) to a *RawCount (wire
// shape). nil stays nil so the JSON omits the field; bucket math
// happens at marshal time via RawCount.MarshalJSON.
func rawCountPtr(p *int) *RawCount {
	if p == nil {
		return nil
	}
	v := RawCount(*p)
	return &v
}

// fileSizeBucket rounds a raw byte count to the schema's
// FileSizeBucket lower bound. Bucket boundaries match the wire
// schema's constraints; values in between map to the nearest
// lower bound.
func fileSizeBucket(bytes int) FileSizeBucket {
	switch {
	case bytes >= int(FileSizeBucketOver100KB):
		return FileSizeBucketOver100KB
	case bytes >= int(FileSizeBucket20To100KB):
		return FileSizeBucket20To100KB
	case bytes >= int(FileSizeBucket5To20KB):
		return FileSizeBucket5To20KB
	case bytes >= int(FileSizeBucket1To5KB):
		return FileSizeBucket1To5KB
	default:
		return FileSizeBucketUnder1KB
	}
}

// shapeContentHash hashes the bucketed wire properties so the
// dedupe key matches what the worker would see. Includes
// Parsable so the unparsable-sentinel doesn't collapse with a
// valid empty projection. JSON encoding is stable for our shape
// (named fields in declaration order, no maps); SHA-256 over
// that gives a deterministic key.
//
// Scope: process-local. The hash is never serialised, never
// compared cross-process, never read by the worker. So we don't
// need a stable cross-language hash - we just need consistency
// within one Client's LSP / dev / MCP lifetime, which is what
// Go's encoding/json provides for a struct of named fields with
// no maps.
//
// Invariant: ProjectionShapeProperties must contain no maps. If
// a future field requires one, sort the keys before hashing or
// switch this function to a stable-encoding hash (gob with
// deterministic types, or a hand-rolled struct walker). Adding a
// map silently breaks dedupe.
func shapeContentHash(props ProjectionShapeProperties) [32]byte {
	b, _ := json.Marshal(props)
	return sha256.Sum256(b)
}

// shapeChangedAndRecord returns true and records the hash if this
// projection_id is new or its content_hash has drifted since the
// last observation. Returns false (and leaves the cache alone)
// when the hash matches the previous entry - the dedupe path.
//
// Bounded by shapeCacheCap via FIFO eviction. Lookup is map O(1);
// eviction shifts a slice which is O(n) but only fires once cap
// is reached, on a path that already emits an envelope (rare
// enough to dominate).
func (c *Client) shapeChangedAndRecord(projectionID string, hash [32]byte) bool {
	c.shapeMu.Lock()
	defer c.shapeMu.Unlock()
	if c.shapeCache == nil {
		c.shapeCache = make(map[string][32]byte, shapeCacheCap)
	}
	prev, exists := c.shapeCache[projectionID]
	if exists && prev == hash {
		return false
	}
	if !exists {
		if len(c.shapeOrder) >= shapeCacheCap {
			// Evict the oldest entry. FIFO is good enough for a
			// dedupe cache where the cost of a false-evict is
			// just one extra envelope.
			delete(c.shapeCache, c.shapeOrder[0])
			c.shapeOrder = c.shapeOrder[1:]
		}
		c.shapeOrder = append(c.shapeOrder, projectionID)
	}
	c.shapeCache[projectionID] = hash
	return true
}
