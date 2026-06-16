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
[[projection]]
name = "cart-count"
entry = "projections/cart-count.js"
engine_version = 2

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

	if cfg.Projection[1].EngineVersion == nil || *cfg.Projection[1].EngineVersion != 1 {
		t.Fatalf("expected engine_version 1, got %v", cfg.Projection[1].EngineVersion)
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
engine_version = 2
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
engine_version = 2
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
engine_version = 2

[[projection]]
name = "test"
entry = "b.js"
engine_version = 2
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
engine_version = 2
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
	cfg := &Config{}

	p := Projection{Name: "a", Entry: "a.js", EngineVersion: ptr(2)}
	if got := cfg.EffectiveEngineVersion(&p); got != 2 {
		t.Fatalf("expected per-projection 2, got %d", got)
	}

	p.EngineVersion = ptr(1)
	if got := cfg.EffectiveEngineVersion(&p); got != 1 {
		t.Fatalf("expected per-projection 1, got %d", got)
	}

	unset := Projection{Name: "b", Entry: "b.js"}
	if got := cfg.EffectiveEngineVersion(&unset); got != 0 {
		t.Fatalf("expected 0 when projection has no engine_version, got %d", got)
	}
}

func TestEffectiveQuirksVersion(t *testing.T) {
	t.Run("env wins", func(t *testing.T) {
		t.Setenv("GAFFER_QUIRKS_VERSION", "26.99.99")
		cfg := &Config{QuirksVersion: "26.0.0"}
		p := Projection{QuirksVersion: "25.5.5"}
		if got := cfg.EffectiveQuirksVersion(&p); got != "26.99.99" {
			t.Fatalf("expected env to win, got %q", got)
		}
	})

	t.Run("projection > config", func(t *testing.T) {
		t.Setenv("GAFFER_QUIRKS_VERSION", "")
		cfg := &Config{QuirksVersion: "26.0.0"}
		p := Projection{QuirksVersion: "26.1.0"}
		if got := cfg.EffectiveQuirksVersion(&p); got != "26.1.0" {
			t.Fatalf("expected projection override, got %q", got)
		}
	})

	t.Run("config fallback", func(t *testing.T) {
		t.Setenv("GAFFER_QUIRKS_VERSION", "")
		cfg := &Config{QuirksVersion: "26.0.0"}
		p := Projection{}
		if got := cfg.EffectiveQuirksVersion(&p); got != "26.0.0" {
			t.Fatalf("expected config fallback, got %q", got)
		}
	})

	t.Run("unset everywhere returns empty", func(t *testing.T) {
		t.Setenv("GAFFER_QUIRKS_VERSION", "")
		cfg := &Config{}
		p := Projection{}
		if got := cfg.EffectiveQuirksVersion(&p); got != "" {
			t.Fatalf("expected empty, got %q", got)
		}
	})
}

func TestLoadValidatesQuirksVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
quirks_version = "not-a-version"

[[projection]]
name = "a"
entry = "a.js"
engine_version = 2
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected validation error for malformed quirks_version")
	}
}

func TestLoadValidatesProjectionQuirksVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
[[projection]]
name = "a"
entry = "a.js"
engine_version = 2
quirks_version = "26.1"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected validation error for malformed projection quirks_version")
	}
}

func TestLoadValidatesEnvQuirksVersion(t *testing.T) {
	t.Setenv("GAFFER_QUIRKS_VERSION", "garbage")
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
[[projection]]
name = "a"
entry = "a.js"
engine_version = 2
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected validation error for malformed GAFFER_QUIRKS_VERSION")
	}
	if !strings.Contains(err.Error(), "GAFFER_QUIRKS_VERSION") {
		t.Errorf("expected error to mention GAFFER_QUIRKS_VERSION, got: %v", err)
	}
}

func TestLoadEnvCert(t *testing.T) {
	const base = `
[[projection]]
name = "a"
entry = "a.js"
engine_version = 2
`
	load := func(t *testing.T, body string) (*Config, error) {
		t.Helper()
		dir := t.TempDir()
		path := filepath.Join(dir, "gaffer.toml")
		if err := os.WriteFile(path, []byte(base+body), 0o644); err != nil {
			t.Fatal(err)
		}
		return Load(path)
	}

	t.Run("both files parse and resolve", func(t *testing.T) {
		cfg, err := load(t, `
[env.staging]
connection = "kurrentdb://staging:2113?tls=true"
user_cert_file = "certs/user.crt"
user_key_file = "certs/user.key"
`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		e := cfg.Env["staging"]
		if e.UserCertFile != "certs/user.crt" || e.UserKeyFile != "certs/user.key" {
			t.Errorf("unexpected cert fields: %+v", e)
		}
		resolved, err := cfg.ResolveEnv("staging")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if resolved.Cert == nil {
			t.Fatal("expected resolved Cert, got nil")
		}
		if resolved.Cert.CertFile != "certs/user.crt" || resolved.Cert.KeyFile != "certs/user.key" {
			t.Errorf("unexpected resolved cert: %+v", resolved.Cert)
		}
	})

	t.Run("surrounding whitespace is trimmed", func(t *testing.T) {
		cfg, err := load(t, `
[env.staging]
connection = "kurrentdb://staging:2113?tls=true"
user_cert_file = "  certs/user.crt  "
user_key_file = "  certs/user.key  "
`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		resolved, _ := cfg.ResolveEnv("staging")
		if resolved.Cert == nil || resolved.Cert.CertFile != "certs/user.crt" || resolved.Cert.KeyFile != "certs/user.key" {
			t.Errorf("expected trimmed cert paths, got %+v", resolved.Cert)
		}
	})

	t.Run("no cert files resolves to nil", func(t *testing.T) {
		cfg, err := load(t, `
[env.plain]
connection = "kurrentdb://plain:2113"
`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		resolved, _ := cfg.ResolveEnv("plain")
		if resolved.Cert != nil {
			t.Errorf("expected nil Cert, got %+v", resolved.Cert)
		}
	})

	t.Run("cert without key is rejected", func(t *testing.T) {
		_, err := load(t, `
[env.staging]
connection = "kurrentdb://staging:2113?tls=true"
user_cert_file = "certs/user.crt"
`)
		if err == nil || !strings.Contains(err.Error(), "set together") {
			t.Fatalf("expected a both-together error, got %v", err)
		}
	})

	t.Run("key without cert is rejected", func(t *testing.T) {
		_, err := load(t, `
[env.staging]
connection = "kurrentdb://staging:2113?tls=true"
user_key_file = "certs/user.key"
`)
		if err == nil || !strings.Contains(err.Error(), "set together") {
			t.Fatalf("expected a both-together error, got %v", err)
		}
	})
}

func TestLoadEnvOAuth(t *testing.T) {
	const base = `
[[projection]]
name = "a"
entry = "a.js"
engine_version = 2
`
	load := func(t *testing.T, body string) (*Config, error) {
		t.Helper()
		dir := t.TempDir()
		path := filepath.Join(dir, "gaffer.toml")
		if err := os.WriteFile(path, []byte(base+body), 0o644); err != nil {
			t.Fatal(err)
		}
		return Load(path)
	}

	t.Run("valid oauth parses", func(t *testing.T) {
		cfg, err := load(t, `
[env.prod]
connection = "kurrentdb://prod:2113"
[env.prod.oauth]
issuer = "https://idp.example.com/realms/kurrent"
client_id = "kurrentdb-client"
scopes = ["openid", "profile"]
audience = "kurrentdb-client"
ca_file = "certs/idp-ca.pem"
`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		o := cfg.Env["prod"].OAuth
		if o == nil {
			t.Fatal("expected oauth config, got nil")
		}
		if o.Issuer != "https://idp.example.com/realms/kurrent" || o.ClientID != "kurrentdb-client" {
			t.Errorf("unexpected issuer/client_id: %+v", o)
		}
		if len(o.Scopes) != 2 || o.Audience != "kurrentdb-client" {
			t.Errorf("unexpected scopes/audience: %+v", o)
		}
		if o.CAFile != "certs/idp-ca.pem" {
			t.Errorf("unexpected ca_file: %q", o.CAFile)
		}
	})

	t.Run("http issuer allowed for loopback", func(t *testing.T) {
		cfg, err := load(t, `
[env.local]
connection = "kurrentdb://localhost:2113?tls=false"
[env.local.oauth]
issuer = "http://localhost:8080/realms/kurrent"
client_id = "kurrentdb-client"
`)
		if err != nil {
			t.Fatalf("unexpected error for loopback http issuer: %v", err)
		}
		if cfg.Env["local"].OAuth == nil {
			t.Fatal("expected oauth config")
		}
	})

	t.Run("env without oauth has nil OAuth", func(t *testing.T) {
		cfg, err := load(t, `
[env.prod]
connection = "kurrentdb://prod:2113"
`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Env["prod"].OAuth != nil {
			t.Error("expected nil OAuth when the section is omitted")
		}
	})

	for _, tc := range []struct {
		name, body, wantErr string
	}{
		{
			name:    "missing issuer",
			body:    "\n[env.prod]\nconnection = \"kurrentdb://prod:2113\"\n[env.prod.oauth]\nclient_id = \"x\"\n",
			wantErr: "missing required field: issuer",
		},
		{
			name:    "missing client_id",
			body:    "\n[env.prod]\nconnection = \"kurrentdb://prod:2113\"\n[env.prod.oauth]\nissuer = \"https://idp.example.com\"\n",
			wantErr: "missing required field: client_id",
		},
		{
			name:    "issuer not an absolute url",
			body:    "\n[env.prod]\nconnection = \"kurrentdb://prod:2113\"\n[env.prod.oauth]\nissuer = \"idp.example.com\"\nclient_id = \"x\"\n",
			wantErr: "issuer must be an absolute URL",
		},
		{
			name:    "http issuer rejected for non-loopback host",
			body:    "\n[env.prod]\nconnection = \"kurrentdb://prod:2113\"\n[env.prod.oauth]\nissuer = \"http://idp.example.com\"\nclient_id = \"x\"\n",
			wantErr: "issuer must use https",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(t, tc.body)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tc.wantErr, err)
			}
		})
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
engine_version = 2
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
[[projection]]
name = "order-count"
entry = "projections/order-count.js"
engine_version = 2
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
[[projection]]
name = "p"
entry = "p.js"
engine_version = 2
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
	content := `[[projection]]
name = "p"
entry = "p.js"
engine_version = 2
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
	content := `[[projection]]
name = "p"
entry = "p.js"
engine_version = 2
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
	content := `[[projection]]
name = "p"
entry = "p.js"
engine_version = 2
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
[[projection]]
name = "p"
entry = "p.js"
engine_version = 2
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
[[projection]]
name = "happy-path"
entry = "p.js"
engine_version = 2
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
			{Name: "test", Entry: "test.js", EngineVersion: ptr(1)},
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

	if loaded.Projection[0].EngineVersion == nil || *loaded.Projection[0].EngineVersion != 1 {
		t.Fatalf("expected engine_version 1, got %v", loaded.Projection[0].EngineVersion)
	}
}

func TestSave_Atomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")

	first := &Config{Projection: []Projection{{Name: "first", Entry: "first.js", EngineVersion: ptr(2)}}}
	if err := Save(path, first); err != nil {
		t.Fatal(err)
	}

	// Tighten the manifest, then overwrite it. Save must go through the
	// rename path, fully replace the content, and preserve the restrictive
	// mode rather than widening it (gaffer.toml may hold credentials).
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	second := &Config{Projection: []Projection{{Name: "second", Entry: "second.js", EngineVersion: ptr(2)}}}
	if err := Save(path, second); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Projection) != 1 || loaded.Projection[0].Name != "second" {
		t.Fatalf("expected the manifest replaced with 'second', got %+v", loaded.Projection)
	}

	// The temp file is renamed (or cleaned up on failure), never left in
	// the project dir for a watcher to trip over.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "gaffer.toml" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected only gaffer.toml in dir, got %v", names)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected Save to preserve mode 0600, got %o", perm)
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
		Projection: []Projection{
			{
				Name:          "checkout",
				Entry:         "checkout.js",
				EngineVersion: ptr(2),
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

// TestMarshalOmitsUnsetEngineVersion guards UI-1635: with EngineVersion
// as a plain int, omitempty did not suppress the zero value and every
// save wrote a spurious `engine_version = 0` line on every projection.
// As *int, an unset version marshals to nothing.
func TestMarshalOmitsUnsetEngineVersion(t *testing.T) {
	cfg := &Config{
		Projection: []Projection{{Name: "a", Entry: "a.js"}},
	}
	data, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "engine_version =") {
		t.Errorf("expected no engine_version line for an unset version, got:\n%s", data)
	}
}

// TestValidateRejectsZeroEngineVersion documents the deliberate
// behaviour change from the UI-1635 fix: an explicit `engine_version = 0`
// is now a distinct, invalid state rather than being silently treated as
// "unset". It must fail with the same "1 or 2" message as any other bad
// version.
func TestValidateRejectsZeroEngineVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
[[projection]]
name = "test"
entry = "test.js"
engine_version = 0
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for engine_version = 0")
	}
	if !strings.Contains(err.Error(), "must be 1 or 2, got 0") {
		t.Errorf("expected \"must be 1 or 2, got 0\", got %q", err.Error())
	}
}

func TestLoadTrackEmittedStreamsRequiresV1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
[[projection]]
name = "p"
entry = "p.js"
engine_version = 2
track_emitted_streams = true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for track_emitted_streams on engine_version 2")
	}
	if !strings.Contains(err.Error(), "track_emitted_streams is only valid with engine_version 1") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadTrackEmittedStreamsAllowedOnV1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
[[projection]]
name = "p"
entry = "p.js"
engine_version = 1
track_emitted_streams = true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.Projection[0].TrackEmittedStreams == nil || !*cfg.Projection[0].TrackEmittedStreams {
		t.Fatalf("expected track_emitted_streams true, got %v", cfg.Projection[0].TrackEmittedStreams)
	}
}

func TestLoadEnvMissingConnection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
[env.local]
default = true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for env with empty connection")
	}
	if !strings.Contains(err.Error(), `env "local" missing required field: connection`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadEnvMultipleDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
[env.local]
connection = "esdb://localhost:2113?tls=false"
default = true

[env.prod]
connection = "esdb://prod:2113"
default = true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for two default envs")
	}
	if !strings.Contains(err.Error(), "only one env may set default = true") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadValidMultiEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
[env.local]
connection = "esdb://localhost:2113?tls=false"
default = true

[env.prod]
connection = "esdb://prod:2113"

[[projection]]
name = "p"
entry = "p.js"
engine_version = 2
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(cfg.Env) != 2 {
		t.Fatalf("expected 2 envs, got %d", len(cfg.Env))
	}
	got := cfg.DefaultEnvConnection()
	if got != "esdb://localhost:2113?tls=false" {
		t.Errorf("DefaultEnvConnection() = %q, want local connection", got)
	}
	resolved, err := cfg.ResolveEnv("prod")
	if err != nil {
		t.Fatalf("ResolveEnv(prod): %v", err)
	}
	if resolved.Connection != "esdb://prod:2113" {
		t.Errorf("ResolveEnv(prod).Connection = %q", resolved.Connection)
	}
}

func TestLoadZeroEnvsWithProjections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
[[projection]]
name = "p"
entry = "p.js"
engine_version = 2
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(cfg.Env) != 0 {
		t.Fatalf("expected 0 envs, got %d", len(cfg.Env))
	}
	if cfg.DefaultEnvConnection() != "" {
		t.Errorf("expected empty default connection, got %q", cfg.DefaultEnvConnection())
	}
}

func TestLoadRejectsRemovedTopLevelConnection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
connection = "esdb://localhost:2113?tls=false"

[[projection]]
name = "p"
entry = "p.js"
engine_version = 2
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected migration error for top-level connection")
	}
	if !strings.Contains(err.Error(), "connection is now per-environment") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadRejectsRemovedTopLevelEngineVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
engine_version = 2

[env.local]
connection = "esdb://localhost:2113?tls=false"
default = true

[[projection]]
name = "p"
entry = "p.js"
engine_version = 2
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected migration error for top-level engine_version")
	}
	if !strings.Contains(err.Error(), "engine_version is now per-projection") {
		t.Errorf("unexpected error: %v", err)
	}
}

// An old single-env gaffer.toml trips both removed keys at once; the
// message names both so the user fixes them in one pass.
func TestLoadReportsAllRemovedTopLevelKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
connection = "esdb://localhost:2113?tls=false"
engine_version = 2

[[projection]]
name = "p"
entry = "p.js"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected migration error")
	}
	if !strings.Contains(err.Error(), "connection is now per-environment") ||
		!strings.Contains(err.Error(), "engine_version is now per-projection") {
		t.Errorf("expected both keys named, got: %v", err)
	}
}

func TestDefaultEnv(t *testing.T) {
	cfg := &Config{Env: map[string]Env{
		"local": {Connection: "esdb://local:2113", Default: true},
		"prod":  {Connection: "esdb://prod:2113"},
	}}
	env, ok := cfg.DefaultEnv()
	if !ok {
		t.Fatal("expected a default env")
	}
	if env.Name != "local" || env.Connection != "esdb://local:2113" {
		t.Fatalf("got %+v, want local default", env)
	}

	// Envs configured but none marked default.
	noDefault := &Config{Env: map[string]Env{"prod": {Connection: "esdb://prod:2113"}}}
	if env, ok := noDefault.DefaultEnv(); ok {
		t.Fatalf("expected no default, got %+v", env)
	}
}

func TestLoadRejectsPathTraversalEnvName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	content := `
[env."../../etc"]
connection = "esdb://localhost:2113"
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for path-significant env name")
	}
	if !strings.Contains(err.Error(), "env name") {
		t.Errorf("unexpected error: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }
