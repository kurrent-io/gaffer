package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
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
