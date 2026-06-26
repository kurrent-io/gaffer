package telemetry

import (
	"reflect"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

func TestManifestFeaturesOf_AllSections(t *testing.T) {
	timeout := 30
	maxState := int64(8388608)
	cfg := &config.Config{
		QuirksVersion: "26.1.0",
		DatabaseConfig: &config.DatabaseConfig{
			CompilationTimeout: &timeout,
			ExecutionTimeout:   &timeout,
			MaxStateSize:       &maxState,
		},
		Env: map[string]config.Env{
			"local": {Connection: "esdb://localhost:2113", Default: true},
		},
		Projection: []config.Projection{
			{Name: "p1", Entry: "p1.js", EngineVersion: new(2), Fixtures: map[string]string{"f": "f.json"}},
			{Name: "p2", Entry: "p2.js", EngineVersion: new(2)},
		},
	}
	got := ManifestFeaturesOf(cfg)
	want := []string{
		"compilation_timeout",
		"engine_version",
		"env",
		"execution_timeout",
		"fixtures",
		"max_state_size",
		"projections",
		"quirks_version",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ManifestFeaturesOf() = %v, want %v", got, want)
	}
}

func TestManifestFeaturesOf_TrackEmittedStreams(t *testing.T) {
	// track_emitted_streams is a v1-only per-projection feature; its
	// presence registers the label.
	track := true
	cfg := &config.Config{
		Projection: []config.Projection{
			{Name: "p", Entry: "p.js", EngineVersion: new(1), TrackEmittedStreams: &track},
		},
	}
	got := ManifestFeaturesOf(cfg)
	want := []string{
		"engine_version",
		"projections",
		"track_emitted_streams",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ManifestFeaturesOf() = %v, want %v", got, want)
	}
}

func TestManifestFeaturesOf_NilConfig(t *testing.T) {
	if got := ManifestFeaturesOf(nil); got != nil {
		t.Errorf("ManifestFeaturesOf(nil) = %v, want nil", got)
	}
}

func TestManifestFeaturesOf_EmptyConfig(t *testing.T) {
	cfg := &config.Config{}
	if got := ManifestFeaturesOf(cfg); len(got) != 0 {
		t.Errorf("ManifestFeaturesOf({}) = %v, want empty", got)
	}
}

func TestManifestFeaturesOf_OnlyProjectionsNoFixtures(t *testing.T) {
	cfg := &config.Config{
		Projection: []config.Projection{{Name: "p", Entry: "p.js"}},
	}
	got := ManifestFeaturesOf(cfg)
	want := []string{"projections"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ManifestFeaturesOf() = %v, want %v", got, want)
	}
}

func TestManifestFeaturesOf_PerProjectionOverridesCount(t *testing.T) {
	// Per-projection engine_version / quirks_version / execution_timeout
	// register the feature even when the top-level key is unset.
	// Reviewer caught this on the prior implementation that only
	// consulted top-level fields.
	timeout := 15
	cfg := &config.Config{
		Projection: []config.Projection{
			{
				Name:             "p",
				Entry:            "p.js",
				EngineVersion:    new(2),
				QuirksVersion:    "26.1.0",
				ExecutionTimeout: &timeout,
			},
		},
	}
	got := ManifestFeaturesOf(cfg)
	want := []string{
		"engine_version",
		"execution_timeout",
		"projections",
		"quirks_version",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ManifestFeaturesOf() = %v, want %v", got, want)
	}
}
