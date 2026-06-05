package config

import (
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestInitProject(t *testing.T) {
	dir := t.TempDir()

	path, err := InitProject(dir, DefaultEngineVersion)
	if err != nil {
		t.Fatalf("InitProject: %v", err)
	}
	if want := filepath.Join(dir, "gaffer.toml"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("loading created project: %v", err)
	}
	if cfg.EngineVersion == nil || *cfg.EngineVersion != 2 {
		t.Errorf("engine version = %v, want 2", cfg.EngineVersion)
	}
}

func TestInitProjectEngineVersion1(t *testing.T) {
	dir := t.TempDir()

	path, err := InitProject(dir, 1)
	if err != nil {
		t.Fatalf("InitProject: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("loading created project: %v", err)
	}
	if cfg.EngineVersion == nil || *cfg.EngineVersion != 1 {
		t.Errorf("engine version = %v, want 1", cfg.EngineVersion)
	}
}

func TestInitProjectRejectsInvalidVersion(t *testing.T) {
	dir := t.TempDir()

	_, err := InitProject(dir, 3)
	if err == nil {
		t.Fatal("expected an error for engine_version 3")
	}
	if !strings.Contains(err.Error(), "must be 1 or 2") {
		t.Errorf("unexpected error: %v", err)
	}
	if _, statErr := Load(filepath.Join(dir, "gaffer.toml")); statErr == nil {
		t.Error("gaffer.toml should not have been created for an invalid version")
	}
}

func TestInitProjectRefusesExisting(t *testing.T) {
	dir := t.TempDir()
	if _, err := InitProject(dir, DefaultEngineVersion); err != nil {
		t.Fatal(err)
	}

	_, err := InitProject(dir, DefaultEngineVersion)
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
			if _, err := InitProject(dir, DefaultEngineVersion); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := successes.Load(); got != 1 {
		t.Fatalf("expected exactly one successful init, got %d", got)
	}
}
