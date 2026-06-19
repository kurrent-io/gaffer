package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/project"
)

func TestLoadEvents_ValidArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.json")
	content := `[
		{"eventType":"A","streamId":"s-1","data":"{}"},
		{"eventType":"B","streamId":"s-2","data":"{}"}
	]`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	events, err := LoadEvents(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
}

func TestLoadEvents_PreservesInt64Precision(t *testing.T) {
	// Soft-delete $metadata fixtures use $tb=long.MaxValue (9223372036854775807)
	// to mark a stream as tombstoned. Without UseNumber the loader rounds
	// through float64 and the marker no longer compares equal, which the
	// runtime then interprets as a malformed metadata event rather than a
	// soft delete.
	dir := t.TempDir()
	path := filepath.Join(dir, "events.json")
	content := `[
		{"eventType":"X","streamId":"s","sequenceNumber":9223372036854775807}
	]`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	events, err := LoadEvents(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(events[0], "9223372036854775807") {
		t.Errorf("int64 max not preserved through round-trip:\n%s", events[0])
	}
}

func TestLoadEvents_EmptyArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.json")
	if err := os.WriteFile(path, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}

	events, err := LoadEvents(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}
}

func TestLoadEvents_NotAnArray(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.json")
	if err := os.WriteFile(path, []byte(`{"eventType":"A"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadEvents(path)
	if err == nil {
		t.Fatal("expected error for non-array JSON")
	}
}

func TestLoadEvents_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadEvents(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadEvents_FileNotFound(t *testing.T) {
	_, err := LoadEvents("/nonexistent/events.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestBuildSessionOptions_EngineVersionFromProjection(t *testing.T) {
	cfg := &config.Config{}
	def := &config.Projection{EngineVersion: ptr(1)}
	proj := NewProjection("/tmp", cfg, def, "")

	opts := buildSessionOptions(proj, false, false)
	if opts == nil {
		t.Fatal("expected options")
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(*opts), &m); err != nil {
		t.Fatal(err)
	}

	if m["engineVersion"] != float64(1) {
		t.Errorf("expected engineVersion 1, got %v", m["engineVersion"])
	}
}

// The [database_config] and per-projection timeouts are declaration-only: they
// describe the expected server config and must never reach gaffer's local
// engine, which is guarded by GAFFER_TIMEOUT_MS instead.
func TestBuildSessionOptions_ConfigTimeoutsNotApplied(t *testing.T) {
	t.Setenv(EnvTimeoutMs, "")
	dbTimeout := 500
	projTimeout := 2000
	cfg := &config.Config{DatabaseConfig: &config.DatabaseConfig{
		CompilationTimeout: &dbTimeout,
		ExecutionTimeout:   &dbTimeout,
	}}
	def := &config.Projection{EngineVersion: ptr(2), ExecutionTimeout: &projTimeout}
	proj := NewProjection("/tmp", cfg, def, "")

	var m map[string]any
	if err := json.Unmarshal([]byte(*buildSessionOptions(proj, false, false)), &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"compilationTimeoutMs", "executionTimeoutMs"} {
		if _, ok := m[key]; ok {
			t.Errorf("config timeouts must not be applied locally; got %s=%v", key, m[key])
		}
	}
}

// GAFFER_TIMEOUT_MS is the local hang-guard, applied to both phases.
func TestBuildSessionOptions_HangGuardFromEnv(t *testing.T) {
	t.Setenv(EnvTimeoutMs, "12000")
	cfg := &config.Config{}
	def := &config.Projection{EngineVersion: ptr(2)}
	proj := NewProjection("/tmp", cfg, def, "")

	var m map[string]any
	if err := json.Unmarshal([]byte(*buildSessionOptions(proj, false, false)), &m); err != nil {
		t.Fatal(err)
	}
	if m["compilationTimeoutMs"] != float64(12000) || m["executionTimeoutMs"] != float64(12000) {
		t.Errorf("expected both timeouts 12000, got compile=%v execute=%v",
			m["compilationTimeoutMs"], m["executionTimeoutMs"])
	}
}

// A non-positive or unparseable GAFFER_TIMEOUT_MS is ignored so the runtime
// applies its built-in default.
func TestBuildSessionOptions_HangGuardIgnoresBadEnv(t *testing.T) {
	for _, v := range []string{"0", "-1", "soon"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv(EnvTimeoutMs, v)
			proj := NewProjection("/tmp", &config.Config{}, &config.Projection{EngineVersion: ptr(2)}, "")
			var m map[string]any
			if err := json.Unmarshal([]byte(*buildSessionOptions(proj, false, false)), &m); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"compilationTimeoutMs", "executionTimeoutMs"} {
				if _, ok := m[key]; ok {
					t.Errorf("expected %s omitted for %q", key, v)
				}
			}
		})
	}
}

func TestBuildSessionOptions_QuirksVersionPassedThroughWhenSet(t *testing.T) {
	t.Setenv("GAFFER_QUIRKS_VERSION", "")
	cfg := &config.Config{QuirksVersion: "26.1.0"}
	def := &config.Projection{Name: "p", Entry: "p.js", EngineVersion: ptr(2)}
	proj := NewProjection("/tmp", cfg, def, "")

	opts := buildSessionOptions(proj, false, false)
	if opts == nil {
		t.Fatal("expected non-nil options")
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(*opts), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if m["quirksVersion"] != "26.1.0" {
		t.Errorf("expected quirksVersion 26.1.0, got %v", m["quirksVersion"])
	}
}

func TestBuildSessionOptions_QuirksVersionOmittedWhenUnset(t *testing.T) {
	t.Setenv("GAFFER_QUIRKS_VERSION", "")
	cfg := &config.Config{}
	def := &config.Projection{Name: "p", Entry: "p.js", EngineVersion: ptr(2)}
	proj := NewProjection("/tmp", cfg, def, "")

	opts := buildSessionOptions(proj, false, false)
	var m map[string]any
	if err := json.Unmarshal([]byte(*opts), &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if _, ok := m["quirksVersion"]; ok {
		t.Errorf("expected quirksVersion to be omitted, got %v", m["quirksVersion"])
	}
}

func TestBuildSessionOptions_AlwaysIncludesEngineVersion(t *testing.T) {
	cfg := &config.Config{}
	def := &config.Projection{EngineVersion: ptr(2)}
	proj := NewProjection("/tmp", cfg, def, "")

	opts := buildSessionOptions(proj, false, false)
	if opts == nil {
		t.Fatal("expected non-nil options - engineVersion is required")
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(*opts), &m); err != nil {
		t.Fatal(err)
	}

	if m["engineVersion"] != float64(2) {
		t.Errorf("expected engineVersion 2, got %v", m["engineVersion"])
	}
}

// max_state_size is the one [database_config] knob enforced locally.
func TestBuildSessionOptions_MaxStateSizeFromDatabaseConfig(t *testing.T) {
	t.Setenv(EnvTimeoutMs, "")
	maxState := int64(8388608)
	cfg := &config.Config{DatabaseConfig: &config.DatabaseConfig{MaxStateSize: &maxState}}
	def := &config.Projection{EngineVersion: ptr(2)}
	proj := NewProjection("/tmp", cfg, def, "")

	var m map[string]any
	if err := json.Unmarshal([]byte(*buildSessionOptions(proj, false, false)), &m); err != nil {
		t.Fatal(err)
	}
	if m["maxStateSizeBytes"] != float64(8388608) {
		t.Errorf("expected max state size 8388608, got %v", m["maxStateSizeBytes"])
	}
}

func TestBuildSessionOptions_MaxStateSizeOmittedWhenUnset(t *testing.T) {
	cfg := &config.Config{}
	def := &config.Projection{EngineVersion: ptr(2)}
	proj := NewProjection("/tmp", cfg, def, "")

	opts := buildSessionOptions(proj, false, false)
	var m map[string]any
	if err := json.Unmarshal([]byte(*opts), &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["maxStateSizeBytes"]; ok {
		t.Errorf("expected maxStateSizeBytes omitted, got %v", m["maxStateSizeBytes"])
	}
}

// A non-positive max_state_size is ignored (the runtime default applies).
func TestBuildSessionOptions_MaxStateSizeOmittedWhenNonPositive(t *testing.T) {
	zero := int64(0)
	cfg := &config.Config{DatabaseConfig: &config.DatabaseConfig{MaxStateSize: &zero}}
	def := &config.Projection{EngineVersion: ptr(2)}
	proj := NewProjection("/tmp", cfg, def, "")

	opts := buildSessionOptions(proj, false, false)
	var m map[string]any
	if err := json.Unmarshal([]byte(*opts), &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["maxStateSizeBytes"]; ok {
		t.Errorf("expected maxStateSizeBytes omitted for non-positive value, got %v", m["maxStateSizeBytes"])
	}
}

func TestReadSource_HappyPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "proj.js"), []byte("fromAll()"), 0o644); err != nil {
		t.Fatal(err)
	}

	src, err := ReadSource(dir, "proj.js")
	if err != nil {
		t.Fatal(err)
	}

	if src != "fromAll()" {
		t.Errorf("expected %q, got %q", "fromAll()", src)
	}
}

func TestReadSource_MissingFile(t *testing.T) {
	dir := t.TempDir()

	_, err := ReadSource(dir, "missing.js")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestNewProjection_SetsFields(t *testing.T) {
	cfg := &config.Config{Env: map[string]config.Env{"local": {Connection: "esdb://localhost:2113", Default: true}}}
	def := &config.Projection{Name: "counts", Entry: "counts.js", EngineVersion: ptr(1)}

	p := NewProjection("/project", cfg, def, "fromAll().when({})")

	if p.Root != "/project" {
		t.Errorf("expected root %q, got %q", "/project", p.Root)
	}
	if p.Config != cfg {
		t.Error("expected Config to match")
	}
	if p.Def != def {
		t.Error("expected Def to match")
	}
	if p.Source != "fromAll().when({})" {
		t.Errorf("expected source %q, got %q", "fromAll().when({})", p.Source)
	}
	if p.EngineVersion != 1 {
		t.Errorf("expected engineVersion 1 (per-projection override), got %d", p.EngineVersion)
	}
}

func TestNewProjection_EngineVersionFromProjection(t *testing.T) {
	// engine_version is per-projection; there is no top-level fallback.
	cfg := &config.Config{}
	def := &config.Projection{Name: "test", Entry: "test.js", EngineVersion: ptr(2)}

	p := NewProjection("/project", cfg, def, "source")

	if p.EngineVersion != 2 {
		t.Errorf("expected engineVersion 2 from projection, got %d", p.EngineVersion)
	}
}

func TestCreateSession_ValidSource(t *testing.T) {
	cfg := &config.Config{}
	def := &config.Projection{Name: "test", Entry: "test.js", EngineVersion: ptr(2)}
	proj := NewProjection("/tmp", cfg, def, `fromAll().when({$init() { return {}; }})`)

	session, sources, err := CreateSession(proj, false, false)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Destroy()

	if !sources.AllStreams {
		t.Error("expected AllStreams to be true for fromAll()")
	}
}

func TestCreateSession_InvalidSource(t *testing.T) {
	cfg := &config.Config{}
	def := &config.Projection{Name: "test", Entry: "test.js"}
	proj := NewProjection("/tmp", cfg, def, "this is not valid javascript {{{")

	_, _, err := CreateSession(proj, false, false)
	if err == nil {
		t.Fatal("expected error for invalid JS source")
	}
}

func TestLoadProjection_ValidProject(t *testing.T) {
	dir := t.TempDir()

	toml := `[[projection]]
name = "counts"
entry = "counts.js"
engine_version = 2
`
	if err := os.WriteFile(filepath.Join(dir, "gaffer.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "counts.js"), []byte("fromAll()"), 0o644); err != nil {
		t.Fatal(err)
	}

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	p, err := LoadProjection("counts")
	if err != nil {
		t.Fatal(err)
	}

	if p.Root != dir {
		t.Errorf("expected root %q, got %q", dir, p.Root)
	}
	if p.Source != "fromAll()" {
		t.Errorf("expected source %q, got %q", "fromAll()", p.Source)
	}
	if p.Def.Name != "counts" {
		t.Errorf("expected projection name %q, got %q", "counts", p.Def.Name)
	}
	if p.EngineVersion != 2 {
		t.Errorf("expected engineVersion 2, got %d", p.EngineVersion)
	}
}

func TestLoadProjection_NotInProject(t *testing.T) {
	dir := t.TempDir()

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	_, err = LoadProjection("anything")
	if err != project.ErrNotInProject {
		t.Errorf("expected ErrNotInProject, got %v", err)
	}
}

func TestLoadProjection_ProjectionNotFound(t *testing.T) {
	dir := t.TempDir()

	toml := `[[projection]]
name = "exists"
entry = "exists.js"
engine_version = 2
`
	if err := os.WriteFile(filepath.Join(dir, "gaffer.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	_, err = LoadProjection("missing")
	if err == nil {
		t.Fatal("expected error for missing projection")
	}
}

func ptr[T any](v T) *T { return &v }
