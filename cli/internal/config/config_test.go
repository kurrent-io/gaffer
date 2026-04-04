package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
[[projection]]
name = "cart-count"
entry = "projections/cart-count.js"

[[projection]]
name = "user-stats"
entry = "projections/user-stats.js"
engine = "v1"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Projection) != 2 {
		t.Fatalf("expected 2 projections, got %d", len(cfg.Projection))
	}

	if cfg.Projection[0].Name != "cart-count" {
		t.Fatalf("expected name cart-count, got %s", cfg.Projection[0].Name)
	}

	if cfg.Projection[1].Engine != "v1" {
		t.Fatalf("expected engine v1, got %s", cfg.Projection[1].Engine)
	}
}

func TestLoadEmptyConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Projection) != 0 {
		t.Fatalf("expected 0 projections, got %d", len(cfg.Projection))
	}
}

func TestLoadMissingName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
[[projection]]
entry = "projections/test.js"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestLoadMissingEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
[[projection]]
name = "test"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing entry")
	}
}

func TestLoadDuplicateNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
[[projection]]
name = "test"
entry = "a.js"

[[projection]]
name = "test"
entry = "b.js"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for duplicate names")
	}
}

func TestLoadPathTraversal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
[[projection]]
name = "evil"
entry = "../../etc/passwd"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}

func TestFindProjection(t *testing.T) {
	cfg := &Config{
		Projection: []Projection{
			{Name: "a", Entry: "a.js"},
			{Name: "b", Entry: "b.js"},
		},
	}

	if p := cfg.FindProjection("a"); p == nil || p.Name != "a" {
		t.Fatal("expected to find projection a")
	}

	if p := cfg.FindProjection("c"); p != nil {
		t.Fatal("expected nil for unknown projection")
	}
}

func TestIsEnabled(t *testing.T) {
	p := Projection{Name: "a", Entry: "a.js"}
	if !p.IsEnabled() {
		t.Fatal("expected enabled by default")
	}

	enabled := true
	p.Enabled = &enabled
	if !p.IsEnabled() {
		t.Fatal("expected enabled when set to true")
	}

	disabled := false
	p.Enabled = &disabled
	if p.IsEnabled() {
		t.Fatal("expected disabled when set to false")
	}
}

func TestEffectiveEngine(t *testing.T) {
	p := Projection{Name: "a", Entry: "a.js"}
	if p.EffectiveEngine() != "v2" {
		t.Fatalf("expected v2 default, got %s", p.EffectiveEngine())
	}

	p.Engine = "v1"
	if p.EffectiveEngine() != "v1" {
		t.Fatalf("expected v1, got %s", p.EffectiveEngine())
	}
}

func TestLoadGlobalTimeouts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
compilation_timeout = 1000
execution_timeout = 500

[[projection]]
name = "test"
entry = "test.js"
execution_timeout = 2000
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.CompilationTimeout == nil || *cfg.CompilationTimeout != 1000 {
		t.Fatal("expected compilation_timeout 1000")
	}
	if cfg.ExecutionTimeout == nil || *cfg.ExecutionTimeout != 500 {
		t.Fatal("expected execution_timeout 500")
	}
	if cfg.Projection[0].ExecutionTimeout == nil || *cfg.Projection[0].ExecutionTimeout != 2000 {
		t.Fatal("expected projection execution_timeout 2000")
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("nonexistent/path/gaffer.toml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadMalformedTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	if err := os.WriteFile(path, []byte("not valid [[ toml = !!!"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for malformed TOML")
	}
}

func TestSaveAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")

	cfg := &Config{
		Projection: []Projection{
			{Name: "test", Entry: "test.js", Engine: "v1"},
		},
	}

	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(loaded.Projection) != 1 {
		t.Fatalf("expected 1 projection, got %d", len(loaded.Projection))
	}

	if loaded.Projection[0].Name != "test" {
		t.Fatalf("expected name test, got %s", loaded.Projection[0].Name)
	}

	if loaded.Projection[0].Engine != "v1" {
		t.Fatalf("expected engine v1, got %s", loaded.Projection[0].Engine)
	}
}
