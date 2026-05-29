package config

import (
	"path/filepath"
	"strings"
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

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("loading created project: %v", err)
	}
	if cfg.EngineVersion != 2 {
		t.Errorf("engine version = %d, want 2", cfg.EngineVersion)
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
