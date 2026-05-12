package telemetry

import (
	"reflect"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

func TestManifestFeaturesOf_AllSections(t *testing.T) {
	timeout := 30
	cfg := &config.Config{
		Connection:         "esdb://localhost:2113",
		EngineVersion:      2,
		DbVersion:          "26.1.0",
		CompilationTimeout: &timeout,
		ExecutionTimeout:   &timeout,
		Projection: []config.Projection{
			{Name: "p1", Entry: "p1.js", Fixtures: map[string]string{"f": "f.json"}},
			{Name: "p2", Entry: "p2.js"},
		},
	}
	got := ManifestFeaturesOf(cfg)
	want := []string{
		"compilation_timeout",
		"connection",
		"db_version",
		"engine_version",
		"execution_timeout",
		"fixtures",
		"projections",
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
	// Per-projection engine_version / db_version / execution_timeout
	// register the feature even when the top-level key is unset.
	// Reviewer caught this on the prior implementation that only
	// consulted top-level fields.
	timeout := 15
	cfg := &config.Config{
		Projection: []config.Projection{
			{
				Name:             "p",
				Entry:            "p.js",
				EngineVersion:    2,
				DbVersion:        "26.1.0",
				ExecutionTimeout: &timeout,
			},
		},
	}
	got := ManifestFeaturesOf(cfg)
	want := []string{
		"db_version",
		"engine_version",
		"execution_timeout",
		"projections",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ManifestFeaturesOf() = %v, want %v", got, want)
	}
}
