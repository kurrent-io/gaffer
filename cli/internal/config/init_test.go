package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestInitProject(t *testing.T) {
	dir := t.TempDir()

	path, err := InitProject(dir)
	if err != nil {
		t.Fatalf("InitProject: %v", err)
	}
	if want := filepath.Join(dir, "gaffer.toml"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}

	// The starter template is entirely commented out: it loads as a
	// valid config with no active envs or projections.
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("loading created project: %v", err)
	}
	if len(cfg.Env) != 0 {
		t.Errorf("expected 0 envs, got %d", len(cfg.Env))
	}
	if len(cfg.Projection) != 0 {
		t.Errorf("expected 0 projections, got %d", len(cfg.Projection))
	}
}

func TestInitProjectTemplateMarkers(t *testing.T) {
	dir := t.TempDir()

	path, err := InitProject(dir)
	if err != nil {
		t.Fatalf("InitProject: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, marker := range []string{"[env.", "[[projection]]", "engine_version = 2"} {
		if !strings.Contains(content, marker) {
			t.Errorf("expected template to document %q, got:\n%s", marker, content)
		}
	}
}

func TestInitProjectRefusesExisting(t *testing.T) {
	dir := t.TempDir()
	if _, err := InitProject(dir); err != nil {
		t.Fatal(err)
	}

	_, err := InitProject(dir)
	if err == nil {
		t.Fatal("expected an error re-initializing an existing project")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestInitProjectConcurrent(t *testing.T) {
	dir := t.TempDir()

	const n = 8
	var wg sync.WaitGroup
	var successes atomic.Int32
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			if _, err := InitProject(dir); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := successes.Load(); got != 1 {
		t.Fatalf("expected exactly one successful init, got %d", got)
	}
}
