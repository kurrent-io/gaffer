package telemetry

import (
	"context"
	"sync"
	"testing"
	"time"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
)

// intPtr is a one-line helper for the *int fields in the binding
// shape. Keeps the test setup readable.
func intPtr(n int) *int { return &n }

// rawShape returns a populated raw FFI shape covering every field
// kind translation should hit (bool, int, *int, FileSize bucket).
// Tests narrow or replace fields per case.
func rawShape() *gafferruntime.ProjectionShape {
	return &gafferruntime.ProjectionShape{
		Parsable: true,
		FileSize: 3000, // 3 KB -> FileSizeBucket1To5KB
		Handlers: gafferruntime.ProjectionShapeHandlers{
			Any:                true,
			Init:               false,
			Deleted:            false,
			DistinctEventNames: 4,
		},
		BuiltinCounts: gafferruntime.ProjectionShapeBuiltinCounts{
			FromAll: intPtr(1),
			When:    intPtr(1),
			Emit:    intPtr(3),
		},
	}
}

func TestTranslateShape_FieldsMapCleanly(t *testing.T) {
	got := translateShape(rawShape(), "abc123")
	if got.ProjectionID != "abc123" {
		t.Errorf("ProjectionID = %q, want abc123", got.ProjectionID)
	}
	if !got.Parsable {
		t.Error("Parsable lost in translation")
	}
	if got.FileSize != FileSizeBucket1To5KB {
		t.Errorf("FileSize = %v, want %v", got.FileSize, FileSizeBucket1To5KB)
	}
	if !got.Handlers.Any || got.Handlers.Init || got.Handlers.Deleted {
		t.Errorf("Handlers bools = %+v, want only Any=true", got.Handlers)
	}
	if got.Handlers.DistinctEventNames != RawCount(4) {
		t.Errorf("DistinctEventNames = %v, want RawCount(4)", got.Handlers.DistinctEventNames)
	}
	if got.BuiltinCounts.FromAll == nil || *got.BuiltinCounts.FromAll != 1 {
		t.Errorf("FromAll = %v, want &1", got.BuiltinCounts.FromAll)
	}
	if got.BuiltinCounts.Emit == nil || *got.BuiltinCounts.Emit != 3 {
		t.Errorf("Emit = %v, want &3", got.BuiltinCounts.Emit)
	}
	if got.BuiltinCounts.OutputState != nil {
		t.Errorf("OutputState = %v, want nil (not set in raw)", got.BuiltinCounts.OutputState)
	}
}

func TestFileSizeBucket_BoundaryRounding(t *testing.T) {
	cases := []struct {
		bytes int
		want  FileSizeBucket
	}{
		{0, FileSizeBucketUnder1KB},
		{1023, FileSizeBucketUnder1KB},
		{1024, FileSizeBucket1To5KB},
		{5119, FileSizeBucket1To5KB},
		{5120, FileSizeBucket5To20KB},
		{20479, FileSizeBucket5To20KB},
		{20480, FileSizeBucket20To100KB},
		{102399, FileSizeBucket20To100KB},
		{102400, FileSizeBucketOver100KB},
		{1_000_000, FileSizeBucketOver100KB},
	}
	for _, tc := range cases {
		if got := fileSizeBucket(tc.bytes); got != tc.want {
			t.Errorf("fileSizeBucket(%d) = %v, want %v", tc.bytes, got, tc.want)
		}
	}
}

func TestShapeContentHash_Stability(t *testing.T) {
	// Same input must produce the same hash across calls - the
	// dedupe relies on this property.
	props := translateShape(rawShape(), "id")
	if shapeContentHash(props) != shapeContentHash(props) {
		t.Error("hash unstable across calls on same input")
	}
}

func TestShapeContentHash_ParsableInDomain(t *testing.T) {
	// Architect's concern from the 10a review: the dedupe hash
	// MUST include Parsable so the parser-drift sentinel
	// ({Parsable: false, all else zero}) does not collapse with
	// a valid empty projection ({Parsable: true, all else zero}).
	unparsable := ProjectionShapeProperties{Parsable: false, ProjectionID: "x"}
	empty := ProjectionShapeProperties{Parsable: true, ProjectionID: "x"}
	if shapeContentHash(unparsable) == shapeContentHash(empty) {
		t.Error("unparsable sentinel collides with empty-parsable shape; hash domain must include Parsable")
	}
}

func TestShapeContentHash_BucketCollapsesSubBucketChanges(t *testing.T) {
	// Two raw shapes with FromAll=2 and FromAll=9 both round to
	// the [2, 10) bucket = RawCount(2) on marshal. The hash is
	// computed over the wire form (post-bucket), so they should
	// produce the same hash - meaning a counter wobble inside a
	// bucket doesn't trigger a redundant emit. This is the whole
	// reason translate happens before hash.
	a := translateShape(&gafferruntime.ProjectionShape{
		Parsable: true,
		BuiltinCounts: gafferruntime.ProjectionShapeBuiltinCounts{FromAll: intPtr(2)},
	}, "x")
	b := translateShape(&gafferruntime.ProjectionShape{
		Parsable: true,
		BuiltinCounts: gafferruntime.ProjectionShapeBuiltinCounts{FromAll: intPtr(9)},
	}, "x")
	// Note: the test asserts the bucketed JSON wire form is
	// identical. The hashes match because RawCount.MarshalJSON
	// emits the bucket lower-bound for both. If this ever fails,
	// either bucket boundaries changed or the marshaler did.
	if shapeContentHash(a) != shapeContentHash(b) {
		t.Error("same-bucket counts produced different hashes; bucket-then-hash invariant broken")
	}
}

func TestEmitProjectionShape_NilClientIsNoop(t *testing.T) {
	// Ctx with no Client: opted-out install path. Helper must
	// not panic and must not record state anywhere.
	EmitProjectionShape(context.Background(), "/some/path", gafferruntime.ProjectionInfo{
		Shape: &gafferruntime.ProjectionShape{Parsable: true},
	})
}

func TestEmitProjectionShape_NilShapeIsNoop(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	EmitProjectionShape(ctx, "/some/path", gafferruntime.ProjectionInfo{Shape: nil})
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if got := mock.Len(); got != 0 {
		t.Errorf("envelopes = %d, want 0 (info.Shape was nil)", got)
	}
}

func TestEmitProjectionShape_EmitsOnFirstEncounter(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	EmitProjectionShape(ctx, "/proj/a.js", gafferruntime.ProjectionInfo{Shape: rawShape()})
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	envs := mock.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envs))
	}
	ps, ok := envs[0].Events[0].(ProjectionShape)
	if !ok {
		t.Fatalf("event = %T, want ProjectionShape", envs[0].Events[0])
	}
	if ps.Name != "projection_shape" {
		t.Errorf("event name = %q, want projection_shape", ps.Name)
	}
	if ps.Properties.ProjectionID == "" {
		t.Error("ProjectionID empty; expected hashed value")
	}
}

func TestEmitProjectionShape_DedupesUnchangedShape(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	for i := 0; i < 3; i++ {
		EmitProjectionShape(ctx, "/proj/a.js", gafferruntime.ProjectionInfo{Shape: rawShape()})
	}
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if got := mock.Len(); got != 1 {
		t.Errorf("envelopes = %d, want 1 (unchanged shape should dedupe)", got)
	}
}

func TestEmitProjectionShape_ReEmitsOnShapeDrift(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	first := rawShape()
	EmitProjectionShape(ctx, "/proj/a.js", gafferruntime.ProjectionInfo{Shape: first})

	// Mutate: add a builtin call. New shape hashes differently;
	// dedupe must NOT suppress.
	drift := rawShape()
	drift.BuiltinCounts.OutputState = intPtr(1)
	EmitProjectionShape(ctx, "/proj/a.js", gafferruntime.ProjectionInfo{Shape: drift})

	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if got := mock.Len(); got != 2 {
		t.Errorf("envelopes = %d, want 2 (first encounter + drift)", got)
	}
}

func TestEmitProjectionShape_DistinctProjectionsBothEmit(t *testing.T) {
	ctx, c, mock := emitTestSetup(t)
	EmitProjectionShape(ctx, "/proj/a.js", gafferruntime.ProjectionInfo{Shape: rawShape()})
	EmitProjectionShape(ctx, "/proj/b.js", gafferruntime.ProjectionInfo{Shape: rawShape()})

	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if got := mock.Len(); got != 2 {
		t.Errorf("envelopes = %d, want 2 (distinct projection paths)", got)
	}
}

func TestShapeCacheEviction_RespectsCap(t *testing.T) {
	// Exercise the FIFO eviction explicitly: insert cap+1
	// distinct entries; assert size stays at cap.
	c := &Client{}
	for i := 0; i < shapeCacheCap+5; i++ {
		// Synthesise unique projection IDs and unique hashes so
		// each iteration adds a new entry rather than dedup-hits.
		id := fakeProjectionID(i)
		var hash [32]byte
		hash[0] = byte(i % 256)
		hash[1] = byte((i / 256) % 256)
		c.shapeChangedAndRecord(id, hash)
	}
	if len(c.shapeCache) != shapeCacheCap {
		t.Errorf("shapeCache size = %d, want %d (FIFO eviction at cap)",
			len(c.shapeCache), shapeCacheCap)
	}
	if len(c.shapeOrder) != shapeCacheCap {
		t.Errorf("shapeOrder length = %d, want %d", len(c.shapeOrder), shapeCacheCap)
	}
}

func TestShapeCacheEviction_FIFOOrder(t *testing.T) {
	// Cap-respect alone isn't enough - the docstring promises FIFO,
	// and someone "improving" it to LRU-touch-on-hit would silently
	// change the contract. Insert cap+1; assert the oldest is gone
	// and a fresh entry replaced it.
	c := &Client{}
	first := fakeProjectionID(0)
	for i := 0; i < shapeCacheCap+1; i++ {
		var hash [32]byte
		hash[0] = byte(i % 256)
		hash[1] = byte((i / 256) % 256)
		c.shapeChangedAndRecord(fakeProjectionID(i), hash)
	}
	if _, present := c.shapeCache[first]; present {
		t.Errorf("first-inserted id %q still in cache after cap+1 inserts; FIFO evicts oldest", first)
	}
	if _, present := c.shapeCache[fakeProjectionID(shapeCacheCap)]; !present {
		t.Errorf("newest id missing from cache; expected to be retained")
	}
}

func TestEmitProjectionShape_AllZeroShapeStillEmitsOnce(t *testing.T) {
	// All-zero shape: no handlers, no builtins, file size 0.
	// translateShape and shapeContentHash must handle this without
	// surprise; emit fires once, repeats dedup.
	ctx, c, mock := emitTestSetup(t)
	zero := &gafferruntime.ProjectionShape{}
	EmitProjectionShape(ctx, "/proj/empty.js", gafferruntime.ProjectionInfo{Shape: zero})
	EmitProjectionShape(ctx, "/proj/empty.js", gafferruntime.ProjectionInfo{Shape: zero})
	if err := c.Flush(timeoutCtx(t, time.Second)); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if got := mock.Len(); got != 1 {
		t.Errorf("all-zero shape: envelopes = %d, want 1 (first emit + dedupe)", got)
	}
}

func TestShapeChangedAndRecord_ConcurrentSafety(t *testing.T) {
	// The mutex is the whole point of putting the cache on Client.
	// Hammer it from many goroutines with distinct IDs; final state
	// must contain exactly N entries. -race catches any unguarded
	// shared-state mutation.
	c := &Client{}
	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			var hash [32]byte
			hash[0] = byte(i)
			c.shapeChangedAndRecord(fakeProjectionID(i), hash)
		}(i)
	}
	wg.Wait()
	if got := len(c.shapeCache); got != n {
		t.Errorf("concurrent inserts: cache size = %d, want %d", got, n)
	}
}

func fakeProjectionID(i int) string {
	// 16 lowercase hex chars matching the projection_id shape.
	// Encodes i as 16 hex digits (zero-padded) so every i yields
	// a distinct id - critical for the eviction test which
	// inserts cap+5 distinct entries.
	const hex = "0123456789abcdef"
	out := make([]byte, 16)
	v := uint64(i)
	for j := 15; j >= 0; j-- {
		out[j] = hex[v&0xf]
		v >>= 4
	}
	return string(out)
}
