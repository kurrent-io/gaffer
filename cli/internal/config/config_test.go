package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
engine_version = 2

[[projection]]
name = "cart-count"
entry = "projections/cart-count.js"

[[projection]]
name = "user-stats"
entry = "projections/user-stats.js"
engine_version = 1
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

	if cfg.Projection[1].EngineVersion != 1 {
		t.Fatalf("expected engine_version 1, got %d", cfg.Projection[1].EngineVersion)
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

func TestLoadMissingEngineVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
[[projection]]
name = "test"
entry = "test.js"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for missing engine_version")
	}
	if !strings.Contains(err.Error(), "engine_version") {
		t.Errorf("expected engine_version in error, got %q", err.Error())
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

func TestEffectiveEngineVersion(t *testing.T) {
	cfg := &Config{EngineVersion: 2}
	p := Projection{Name: "a", Entry: "a.js"}
	if got := cfg.EffectiveEngineVersion(&p); got != 2 {
		t.Fatalf("expected top-level 2, got %d", got)
	}

	p.EngineVersion = 1
	if got := cfg.EffectiveEngineVersion(&p); got != 1 {
		t.Fatalf("expected per-projection override 1, got %d", got)
	}

	emptyCfg := &Config{}
	emptyP := Projection{Name: "b", Entry: "b.js"}
	if got := emptyCfg.EffectiveEngineVersion(&emptyP); got != 0 {
		t.Fatalf("expected 0 when neither set, got %d", got)
	}
}

func TestEffectiveDbVersion(t *testing.T) {
	t.Run("env wins", func(t *testing.T) {
		t.Setenv("GAFFER_DB_VERSION", "26.99.99")
		cfg := &Config{DbVersion: "26.0.0"}
		p := Projection{DbVersion: "25.5.5"}
		if got := cfg.EffectiveDbVersion(&p); got != "26.99.99" {
			t.Fatalf("expected env to win, got %q", got)
		}
	})

	t.Run("projection > config", func(t *testing.T) {
		t.Setenv("GAFFER_DB_VERSION", "")
		cfg := &Config{DbVersion: "26.0.0"}
		p := Projection{DbVersion: "26.1.0"}
		if got := cfg.EffectiveDbVersion(&p); got != "26.1.0" {
			t.Fatalf("expected projection override, got %q", got)
		}
	})

	t.Run("config fallback", func(t *testing.T) {
		t.Setenv("GAFFER_DB_VERSION", "")
		cfg := &Config{DbVersion: "26.0.0"}
		p := Projection{}
		if got := cfg.EffectiveDbVersion(&p); got != "26.0.0" {
			t.Fatalf("expected config fallback, got %q", got)
		}
	})

	t.Run("unset everywhere returns empty", func(t *testing.T) {
		t.Setenv("GAFFER_DB_VERSION", "")
		cfg := &Config{}
		p := Projection{}
		if got := cfg.EffectiveDbVersion(&p); got != "" {
			t.Fatalf("expected empty, got %q", got)
		}
	})
}

func TestLoadValidatesDbVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
engine_version = 2
db_version = "not-a-version"

[[projection]]
name = "a"
entry = "a.js"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected validation error for malformed db_version")
	}
}

func TestLoadValidatesProjectionDbVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
engine_version = 2

[[projection]]
name = "a"
entry = "a.js"
db_version = "26.1"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected validation error for malformed projection db_version")
	}
}

func TestLoadValidatesEnvDbVersion(t *testing.T) {
	t.Setenv("GAFFER_DB_VERSION", "garbage")
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
engine_version = 2

[[projection]]
name = "a"
entry = "a.js"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for malformed GAFFER_DB_VERSION")
	}
	if !strings.Contains(err.Error(), "GAFFER_DB_VERSION") {
		t.Errorf("expected error to mention GAFFER_DB_VERSION, got: %v", err)
	}
}

func TestLoadGlobalTimeouts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
engine_version = 2
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

func TestLoadFixtures(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
engine_version = 2

[[projection]]
name = "order-count"
entry = "projections/order-count.js"
fixtures.happy-path = "fixtures/happy.json"
fixtures.edge-cases = "fixtures/edge.json"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	p := cfg.Projection[0]
	if len(p.Fixtures) != 2 {
		t.Fatalf("expected 2 fixtures, got %d", len(p.Fixtures))
	}
	if p.Fixtures["happy-path"] != "fixtures/happy.json" {
		t.Fatalf("unexpected fixtures[happy-path]: %q", p.Fixtures["happy-path"])
	}
	if got, ok := p.FindFixture("edge-cases"); !ok || got != "fixtures/edge.json" {
		t.Fatalf("FindFixture lookup failed: %q ok=%v", got, ok)
	}
	if _, ok := p.FindFixture("missing"); ok {
		t.Fatal("expected ok=false for unknown fixture")
	}
	// FixtureNames is sorted; declaration order is irrelevant since the
	// underlying TOML representation is a map.
	if names := p.FixtureNames(); len(names) != 2 || names[0] != "edge-cases" || names[1] != "happy-path" {
		t.Fatalf("FixtureNames mismatch: %v", names)
	}
}

func TestLoadFixtures_DuplicateName(t *testing.T) {
	// Duplicate fixtures.<name> entries are caught by the TOML parser
	// itself as a duplicate-key error - no validate() rule needed.
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
engine_version = 2

[[projection]]
name = "p"
entry = "p.js"
fixtures.dup = "a.json"
fixtures.dup = "b.json"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected duplicate-key parse error")
	}
}

func TestLoadFixtures_EmptyName(t *testing.T) {
	// fixtures."" = "x.json" parses as a quoted-empty key. The map
	// lookup would silently treat this as nameless. Reject explicitly.
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `engine_version = 2
[[projection]]
name = "p"
entry = "p.js"
fixtures."" = "x.json"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "empty name") {
		t.Fatalf("expected empty-name error, got %v", err)
	}
}

func TestLoadFixtures_EmptyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `engine_version = 2
[[projection]]
name = "p"
entry = "p.js"
fixtures.empty = ""
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "empty path") {
		t.Fatalf("expected empty-path error, got %v", err)
	}
}

func TestLoadFixtures_InternalDotDotResolvesInsideRoot(t *testing.T) {
	// fixtures/sub/../happy.json resolves to fixtures/happy.json -
	// still inside the project root, must be accepted. Only paths
	// whose Clean form starts with ".." are escapes.
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `engine_version = 2
[[projection]]
name = "p"
entry = "p.js"
fixtures.happy = "fixtures/sub/../happy.json"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestLoadFixtures_PathEscape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
engine_version = 2

[[projection]]
name = "p"
entry = "p.js"
fixtures.evil = "../outside.json"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "escape project root") {
		t.Fatalf("expected path-escape error, got %v", err)
	}
}

func TestLoadFixtures_NameMatchesProjection(t *testing.T) {
	// A fixture named the same as its parent projection should be allowed.
	// They live in different namespaces (projection name in --, fixture in --fixture).
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
engine_version = 2

[[projection]]
name = "happy-path"
entry = "p.js"
fixtures.happy-path = "fixtures/happy.json"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got, ok := cfg.Projection[0].FindFixture("happy-path"); !ok || got != "fixtures/happy.json" {
		t.Fatalf("expected fixture lookup to succeed, got %q ok=%v", got, ok)
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
			{Name: "test", Entry: "test.js", EngineVersion: 1},
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

	if loaded.Projection[0].EngineVersion != 1 {
		t.Fatalf("expected engine_version 1, got %d", loaded.Projection[0].EngineVersion)
	}
}

func TestSaveAndReload_Fixtures(t *testing.T) {
	// Round-trip: encoding the Fixtures map through toml.NewEncoder
	// and decoding back must preserve names and paths. Without this
	// test a regression that drops the map on save (or scrambles
	// keys) would only surface in production.
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")

	cfg := &Config{
		EngineVersion: 2,
		Projection: []Projection{
			{
				Name:  "checkout",
				Entry: "checkout.js",
				Fixtures: map[string]string{
					"happy": "fixtures/orders.json",
					"full":  "fixtures/orders-full.json",
				},
			},
		},
	}

	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	got := loaded.Projection[0].Fixtures
	if len(got) != 2 {
		t.Fatalf("expected 2 fixtures, got %d", len(got))
	}
	if got["happy"] != "fixtures/orders.json" {
		t.Errorf("happy: got %q, want fixtures/orders.json", got["happy"])
	}
	if got["full"] != "fixtures/orders-full.json" {
		t.Errorf("full: got %q, want fixtures/orders-full.json", got["full"])
	}
}

func TestFixtureCount_Totals(t *testing.T) {
	cfg := &Config{
		Projection: []Projection{
			{Name: "a", Entry: "a.js", Fixtures: map[string]string{"x": "x", "y": "y"}},
			{Name: "b", Entry: "b.js"},
			{Name: "c", Entry: "c.js", Fixtures: map[string]string{"z": "z"}},
		},
	}
	if got := cfg.FixtureCount(); got != 3 {
		t.Errorf("FixtureCount() = %d, want 3", got)
	}
}

func TestProjectionCount(t *testing.T) {
	cfg := &Config{Projection: []Projection{{Name: "a", Entry: "a.js"}, {Name: "b", Entry: "b.js"}}}
	if got := cfg.ProjectionCount(); got != 2 {
		t.Errorf("ProjectionCount() = %d, want 2", got)
	}
	empty := &Config{}
	if got := empty.ProjectionCount(); got != 0 {
		t.Errorf("ProjectionCount() on empty = %d, want 0", got)
	}
}
