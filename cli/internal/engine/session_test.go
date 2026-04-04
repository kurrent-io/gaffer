package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestBuildSessionOptions_EngineOnly(t *testing.T) {
	cfg := &config.Config{}
	proj := &config.Projection{Engine: "v1"}

	opts := buildSessionOptions(cfg, proj, false)
	if opts == nil {
		t.Fatal("expected options")
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(*opts), &m); err != nil {
		t.Fatal(err)
	}

	if m["version"] != "v1" {
		t.Errorf("expected version v1, got %v", m["version"])
	}
}

func TestBuildSessionOptions_ProjectionTimeoutOverridesGlobal(t *testing.T) {
	globalTimeout := 500
	projTimeout := 2000
	cfg := &config.Config{ExecutionTimeout: &globalTimeout}
	proj := &config.Projection{ExecutionTimeout: &projTimeout}

	opts := buildSessionOptions(cfg, proj, false)
	if opts == nil {
		t.Fatal("expected options")
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(*opts), &m); err != nil {
		t.Fatal(err)
	}

	if m["executionTimeoutMs"] != float64(2000) {
		t.Errorf("expected execution timeout 2000, got %v", m["executionTimeoutMs"])
	}
}

func TestBuildSessionOptions_NoOptions(t *testing.T) {
	cfg := &config.Config{}
	proj := &config.Projection{}

	opts := buildSessionOptions(cfg, proj, false)
	if opts != nil {
		t.Error("expected nil when no options set")
	}
}

func TestBuildSessionOptions_GlobalFallback(t *testing.T) {
	execTimeout := 500
	compTimeout := 1000
	cfg := &config.Config{
		ExecutionTimeout:   &execTimeout,
		CompilationTimeout: &compTimeout,
	}
	proj := &config.Projection{}

	opts := buildSessionOptions(cfg, proj, false)
	if opts == nil {
		t.Fatal("expected options")
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(*opts), &m); err != nil {
		t.Fatal(err)
	}

	if m["executionTimeoutMs"] != float64(500) {
		t.Errorf("expected execution timeout 500, got %v", m["executionTimeoutMs"])
	}
	if m["compilationTimeoutMs"] != float64(1000) {
		t.Errorf("expected compilation timeout 1000, got %v", m["compilationTimeoutMs"])
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
	cfg := &config.Config{Connection: "esdb://localhost:2113"}
	def := &config.Projection{Name: "counts", Entry: "counts.js", Engine: "v1"}

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
	if p.Engine != "v1" {
		t.Errorf("expected engine %q, got %q", "v1", p.Engine)
	}
}

func TestNewProjection_DefaultEngine(t *testing.T) {
	cfg := &config.Config{}
	def := &config.Projection{Name: "test", Entry: "test.js"}

	p := NewProjection("/project", cfg, def, "source")

	if p.Engine != config.DefaultEngine {
		t.Errorf("expected default engine %q, got %q", config.DefaultEngine, p.Engine)
	}
}

func TestCreateSession_ValidSource(t *testing.T) {
	cfg := &config.Config{}
	def := &config.Projection{Name: "test", Entry: "test.js"}
	proj := NewProjection("/tmp", cfg, def, `fromAll().when({$init: function() { return {}; }})`)

	session, sources, err := CreateSession(proj, false)
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

	_, _, err := CreateSession(proj, false)
	if err == nil {
		t.Fatal("expected error for invalid JS source")
	}
}

func TestLoadProjection_ValidProject(t *testing.T) {
	dir := t.TempDir()

	toml := `[[projection]]
name = "counts"
entry = "counts.js"
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
	if p.Engine != config.DefaultEngine {
		t.Errorf("expected engine %q, got %q", config.DefaultEngine, p.Engine)
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
