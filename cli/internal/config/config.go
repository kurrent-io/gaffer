package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/kurrent-io/gaffer/cli/internal/project"
)

// ErrManifestParse wraps TOML-level parse failures from Load / Parse.
// Callers use errors.Is to classify the outcome for telemetry without
// pattern-matching on formatted error strings.
var ErrManifestParse = errors.New("parse gaffer.toml")

// ErrManifestValidate wraps validation failures from Load.validate()
// (the file parsed but semantic checks rejected it). Callers use
// errors.Is to distinguish "broken TOML" from "TOML the schema
// rejected".
var ErrManifestValidate = errors.New("validate gaffer.toml")

// Config represents a gaffer.toml file.
type Config struct {
	Connection         string       `toml:"connection,omitempty"`
	EngineVersion      int          `toml:"engine_version,omitempty"`
	DbVersion          string       `toml:"db_version,omitempty"`
	CompilationTimeout *int         `toml:"compilation_timeout,omitempty"`
	ExecutionTimeout   *int         `toml:"execution_timeout,omitempty"`
	Projection         []Projection `toml:"projection"`
}

// Projection is a single projection entry in the config.
//
// Fixtures is a name -> path map, declared in the toml as
// `fixtures.<name> = "<path>"`. Paths are resolved relative to the
// project root, same as Projection.Entry.
type Projection struct {
	Name             string            `toml:"name"`
	Entry            string            `toml:"entry"`
	EngineVersion    int               `toml:"engine_version,omitempty"`
	DbVersion        string            `toml:"db_version,omitempty"`
	Enabled          *bool             `toml:"enabled,omitempty"`
	ExecutionTimeout *int              `toml:"execution_timeout,omitempty"`
	Fixtures         map[string]string `toml:"fixtures,omitempty"`
}

// FindFixture returns the path of the named fixture and true, or "" and
// false if no such fixture exists.
func (p *Projection) FindFixture(name string) (string, bool) {
	path, ok := p.Fixtures[name]
	return path, ok
}

// FixtureNames returns the declared fixture names in alphabetical order
// (TOML map iteration is unordered).
func (p *Projection) FixtureNames() []string {
	names := make([]string, 0, len(p.Fixtures))
	for n := range p.Fixtures {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ProjectionCount returns the number of projections declared in the
// manifest. Stamped on `dev` and `manifest` command_invoked events as
// a raw count; the schema's bucketing happens at marshal time.
func (c *Config) ProjectionCount() int {
	return len(c.Projection)
}

// FixtureCount returns the total number of fixtures declared across
// all projections in the manifest. Same telemetry path as
// ProjectionCount.
func (c *Config) FixtureCount() int {
	total := 0
	for _, p := range c.Projection {
		total += len(p.Fixtures)
	}
	return total
}

// EffectiveEngineVersion returns the projection's engine_version, falling
// back to the top-level engine_version. Returns 0 if neither is set.
func (c *Config) EffectiveEngineVersion(p *Projection) int {
	if p.EngineVersion != 0 {
		return p.EngineVersion
	}
	return c.EngineVersion
}

// EffectiveDbVersion returns the effective KurrentDB target version for the
// given projection. Resolution order: GAFFER_DB_VERSION env var > projection's
// db_version > top-level db_version > "". Empty string means "unversioned":
// gaffer matches every known KurrentDB quirk.
func (c *Config) EffectiveDbVersion(p *Projection) string {
	if v := os.Getenv("GAFFER_DB_VERSION"); v != "" {
		return v
	}
	if p != nil && p.DbVersion != "" {
		return p.DbVersion
	}
	return c.DbVersion
}

// dbVersionPattern matches MAJOR.MINOR.PATCH (e.g. "26.1.0"). Mirrors the
// runtime's KurrentDbVersion.TryParse so we can fail-fast at config load.
var dbVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func validDbVersion(s string) bool {
	return dbVersionPattern.MatchString(s)
}

// IsEnabled returns true if the projection is enabled (default true).
func (p Projection) IsEnabled() bool {
	if p.Enabled == nil {
		return true
	}
	return *p.Enabled
}

// LoadFromCwd resolves the project root from the current working
// directory via project.FindRoot and loads its gaffer.toml. Returns
// project.ErrNotInProject when no project is found; other errors come
// from Load (read or parse failure). Callers that want a best-effort
// load can discard the error.
func LoadFromCwd() (*Config, error) {
	root := project.FindRoot()
	if root == "" {
		return nil, project.ErrNotInProject
	}
	return Load(project.ConfigPath(root))
}

// Load reads and parses a gaffer.toml file with strict validation.
// The loose-validation counterpart used by the LSP server is
// Describe, which shares this function's parse step via Parse.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrManifestValidate, err)
	}
	return cfg, nil
}

// Parse decodes raw config bytes into a Config without running
// validation. Shared by Load (which then runs strict validate())
// and Describe (which runs loose per-element checks). Callers that
// want the file's content from disk should use Load; Parse is for
// in-memory bytes (LSP didChange flow, tests).
func Parse(data []byte) (*Config, error) {
	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrManifestParse, err)
	}
	return &cfg, nil
}

// Save writes the config to a gaffer.toml file.
func Save(path string, cfg *Config) error {
	var sb strings.Builder
	if err := toml.NewEncoder(&sb).Encode(cfg); err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// FindProjection returns the projection with the given name, or nil.
func (c *Config) FindProjection(name string) *Projection {
	for i := range c.Projection {
		if c.Projection[i].Name == name {
			return &c.Projection[i]
		}
	}
	return nil
}

func (c *Config) validate() error {
	if c.EngineVersion != 0 && c.EngineVersion != 1 && c.EngineVersion != 2 {
		return fmt.Errorf("engine_version must be 1 or 2, got %d", c.EngineVersion)
	}
	if c.DbVersion != "" && !validDbVersion(c.DbVersion) {
		return fmt.Errorf("db_version %q must be MAJOR.MINOR.PATCH (e.g. %q)", c.DbVersion, "26.1.0")
	}
	// GAFFER_DB_VERSION overrides every db_version in the file, so an
	// invalid value would silently invalidate the entire config without
	// validation here. Fail fast at the same gate as the file values.
	if v := os.Getenv("GAFFER_DB_VERSION"); v != "" && !validDbVersion(v) {
		return fmt.Errorf("GAFFER_DB_VERSION %q must be MAJOR.MINOR.PATCH (e.g. %q)", v, "26.1.0")
	}
	seen := make(map[string]bool)
	for _, p := range c.Projection {
		// Shared with Describe via checkProjection - rule list and
		// ordering live in validation.go so the loose path can't drift.
		if _, msg, fail := checkProjection(p); fail {
			return fmt.Errorf("%s", msg)
		}
		// Strict-only checks: engine_version, duplicate-name. Loose
		// path either doesn't surface them (engine_version) or
		// handles them post-loop with cross-element state
		// (duplicate-name).
		if p.EngineVersion != 0 && p.EngineVersion != 1 && p.EngineVersion != 2 {
			return fmt.Errorf("projection %q engine_version must be 1 or 2, got %d", p.Name, p.EngineVersion)
		}
		if p.DbVersion != "" && !validDbVersion(p.DbVersion) {
			return fmt.Errorf("projection %q db_version %q must be MAJOR.MINOR.PATCH (e.g. %q)", p.Name, p.DbVersion, "26.1.0")
		}
		if c.EffectiveEngineVersion(&p) == 0 {
			return fmt.Errorf("projection %q has no engine_version set (also missing top-level engine_version)", p.Name)
		}
		if seen[p.Name] {
			return fmt.Errorf("duplicate projection name: %q", p.Name)
		}
		seen[p.Name] = true

		// Iterate in sorted order so error messages are stable.
		for _, name := range p.FixtureNames() {
			if _, msg, fail := checkFixture(p.Name, name, p.Fixtures[name]); fail {
				return fmt.Errorf("%s", msg)
			}
		}
	}
	return nil
}
