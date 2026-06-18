package engine

import (
	"testing"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
)

func TestShapeEmits(t *testing.T) {
	counts := func(set func(*gafferruntime.ProjectionShapeBuiltinCounts)) *gafferruntime.ProjectionShape {
		s := &gafferruntime.ProjectionShape{}
		set(&s.BuiltinCounts)
		return s
	}
	for _, tc := range []struct {
		name  string
		shape *gafferruntime.ProjectionShape
		want  bool
	}{
		{"nil shape", nil, false},
		{"no writes", &gafferruntime.ProjectionShape{}, false},
		{"emit", counts(func(c *gafferruntime.ProjectionShapeBuiltinCounts) { c.Emit = ptr(1) }), true},
		{"linkTo", counts(func(c *gafferruntime.ProjectionShapeBuiltinCounts) { c.LinkTo = ptr(2) }), true},
		{"copyTo", counts(func(c *gafferruntime.ProjectionShapeBuiltinCounts) { c.CopyTo = ptr(1) }), true},
		{"zero count is not emit", counts(func(c *gafferruntime.ProjectionShapeBuiltinCounts) { c.Emit = ptr(0) }), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shapeEmits(tc.shape); got != tc.want {
				t.Errorf("shapeEmits = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLocalDescriptor(t *testing.T) {
	const emitting = `fromAll().when({ $any: function (s, e) { emit('out', 'T', {}); return s; } })`
	const plain = `fromAll().when({ $init: function () { return { n: 0 }; }, $any: function (s, e) { s.n++; return s; } })`

	for _, tc := range []struct {
		name     string
		source   string
		engine   int
		track    *bool
		wantEmit bool
	}{
		{"emitting projection", emitting, 2, nil, true},
		{"non-emitting projection", plain, 2, nil, false},
		{"track emitted streams from config", plain, 1, ptr(true), false}, // track is V1-only
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			def := &config.Projection{Name: "p", Entry: "p.js", EngineVersion: ptr(tc.engine), TrackEmittedStreams: tc.track}
			proj := NewProjection("/tmp", cfg, def, tc.source)

			desc, err := LocalDescriptor(proj)
			if err != nil {
				t.Fatalf("LocalDescriptor: %v", err)
			}
			if desc.Query != tc.source {
				t.Errorf("Query = %q, want the raw source", desc.Query)
			}
			if desc.EngineVersion != tc.engine {
				t.Errorf("EngineVersion = %d, want %d", desc.EngineVersion, tc.engine)
			}
			if desc.Emit != tc.wantEmit {
				t.Errorf("Emit = %v, want %v", desc.Emit, tc.wantEmit)
			}
			if want := tc.track != nil && *tc.track; desc.TrackEmittedStreams != want {
				t.Errorf("TrackEmittedStreams = %v, want %v", desc.TrackEmittedStreams, want)
			}
		})
	}
}
