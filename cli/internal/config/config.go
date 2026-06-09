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
	QuirksVersion      string         `toml:"quirks_version,omitempty"`
	CompilationTimeout *int           `toml:"compilation_timeout,omitempty"`
	ExecutionTimeout   *int           `toml:"execution_timeout,omitempty"`
	Env                map[string]Env `toml:"env,omitempty"`
	Projection         []Projection   `toml:"projection"`
}

// Env is a named deployment target, declared as `[env.<name>]`. Each
// env is self-contained: it carries its own connection and inherits
// nothing from the top level or other envs. At most one env may set
// default = true; it's used when --env is omitted.
type Env struct {
	Connection string `toml:"connection"`
	Default    bool   `toml:"default,omitempty"`
}

// Projection is a single projection entry in the config.
//
// EngineVersion is required (no top-level fallback); validate()
// rejects a projection without it. TrackEmittedStreams is optional and
// valid only on the v1 engine.
//
// Fixtures is a name -> path map, declared in the toml as
// `fixtures.<name> = "<path>"`. Paths are resolved relative to the
// project root, same as Projection.Entry.
type Projection struct {
	Name                string            `toml:"name"`
	Entry               string            `toml:"entry"`
	EngineVersion       *int              `toml:"engine_version,omitempty"`
	TrackEmittedStreams *bool             `toml:"track_emitted_streams,omitempty"`
	QuirksVersion       string            `toml:"quirks_version,omitempty"`
	ExecutionTimeout    *int              `toml:"execution_timeout,omitempty"`
	Fixtures            map[string]string `toml:"fixtures,omitempty"`
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

// EffectiveEngineVersion returns the projection's engine_version. After
// a successful Load it is always set (validate() requires it); returns
// 0 only for a nil projection or a config that bypassed validation.
func (c *Config) EffectiveEngineVersion(p *Projection) int {
	if p != nil && p.EngineVersion != nil {
		return *p.EngineVersion
	}
	return 0
}

// EffectiveQuirksVersion returns the effective KurrentDB quirks-matching version for the
// given projection. Resolution order: GAFFER_QUIRKS_VERSION env var > projection's
// quirks_version > top-level quirks_version > "". Empty string means "unversioned":
// gaffer matches every known KurrentDB quirk.
func (c *Config) EffectiveQuirksVersion(p *Projection) string {
	if v := os.Getenv("GAFFER_QUIRKS_VERSION"); v != "" {
		return v
	}
	if p != nil && p.QuirksVersion != "" {
		return p.QuirksVersion
	}
	return c.QuirksVersion
}

// quirksVersionPattern matches MAJOR.MINOR.PATCH (e.g. "26.1.0"). Mirrors the
// runtime's KurrentDbVersion.TryParse so we can fail-fast at config load.
var quirksVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func validQuirksVersion(s string) bool {
	return quirksVersionPattern.MatchString(s)
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
	cfg, md, err := decode(data)
	if err != nil {
		return nil, err
	}
	// Removed-key migration hints run before validate() so an upgraded
	// project with an old gaffer.toml gets pointed at the new schema
	// instead of a downstream "no environments" / "missing
	// engine_version" error that doesn't name the cause.
	if err := checkRemovedKeys(md); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrManifestValidate, err)
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
	cfg, _, err := decode(data)
	return cfg, err
}

// decode unmarshals raw config bytes, returning the decoder metadata so
// callers can inspect which keys went unmatched (used for removed-key
// migration hints). Wraps TOML syntax errors as ErrManifestParse.
func decode(data []byte) (*Config, toml.MetaData, error) {
	var cfg Config
	md, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return nil, md, fmt.Errorf("%w: %s", ErrManifestParse, err)
	}
	return &cfg, md, nil
}

// removedTopLevelKeys maps a top-level key that gaffer.toml used to
// accept to its migration message. Each message leads with what
// changed, then how to fix it. The multi-environment restructure
// dropped both; the TOML decoder silently ignores unknown keys, so
// without this an old file's connection just vanishes.
var removedTopLevelKeys = map[string]string{
	"connection":     "connection is now per-environment. Move it into an [env.<name>] block, and set `default = true` for auto-selection.",
	"engine_version": "engine_version is now per-projection. Set it on each [[projection]].",
}

// checkRemovedKeys reports any removed top-level keys found in the
// decoded-but-unmatched set, with migration advice. Only top-level
// scalars are considered, so an [env.*] connection (legitimately
// decoded) or an unrelated nested key never trips it. Multiple hits
// are listed one per line in sorted order for a stable message.
func checkRemovedKeys(md toml.MetaData) error {
	var msgs []string
	for _, key := range md.Undecoded() {
		if len(key) != 1 {
			continue
		}
		if advice, ok := removedTopLevelKeys[key[0]]; ok {
			msgs = append(msgs, advice)
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	sort.Strings(msgs)
	return errors.New(strings.Join(msgs, "\n"))
}

// Marshal encodes the config to TOML bytes.
func Marshal(cfg *Config) ([]byte, error) {
	var sb strings.Builder
	if err := toml.NewEncoder(&sb).Encode(cfg); err != nil {
		return nil, fmt.Errorf("encoding config: %w", err)
	}
	return []byte(sb.String()), nil
}

// Save writes the config to a gaffer.toml file.
func Save(path string, cfg *Config) error {
	data, err := Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
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
	if c.QuirksVersion != "" && !validQuirksVersion(c.QuirksVersion) {
		return fmt.Errorf("quirks_version %q must be MAJOR.MINOR.PATCH (e.g. %q)", c.QuirksVersion, "26.1.0")
	}
	// GAFFER_QUIRKS_VERSION overrides every quirks_version in the file, so an
	// invalid value would silently invalidate the entire config without
	// validation here. Fail fast at the same gate as the file values.
	if v := os.Getenv("GAFFER_QUIRKS_VERSION"); v != "" && !validQuirksVersion(v) {
		return fmt.Errorf("GAFFER_QUIRKS_VERSION %q must be MAJOR.MINOR.PATCH (e.g. %q)", v, "26.1.0")
	}
	if err := c.validateEnvs(); err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, p := range c.Projection {
		// Shared with Describe via checkProjection - rule list and
		// ordering live in validation.go so the loose path can't drift.
		if _, msg, fail := checkProjection(p); fail {
			return fmt.Errorf("%s", msg)
		}
		// Strict-only checks: engine_version, track_emitted_streams,
		// duplicate-name. The loose path either doesn't surface them
		// or handles them post-loop with cross-element state.
		if p.EngineVersion == nil {
			return fmt.Errorf("projection %q missing required field: engine_version", p.Name)
		}
		if *p.EngineVersion != 1 && *p.EngineVersion != 2 {
			return fmt.Errorf("projection %q engine_version must be 1 or 2, got %d", p.Name, *p.EngineVersion)
		}
		if p.TrackEmittedStreams != nil && *p.EngineVersion != 1 {
			return fmt.Errorf("projection %q track_emitted_streams is only valid with engine_version 1", p.Name)
		}
		if p.QuirksVersion != "" && !validQuirksVersion(p.QuirksVersion) {
			return fmt.Errorf("projection %q quirks_version %q must be MAJOR.MINOR.PATCH (e.g. %q)", p.Name, p.QuirksVersion, "26.1.0")
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

// validateEnvs checks the [env.*] blocks: each env must carry a
// non-empty connection, and at most one may set default = true. Zero
// envs is valid - a project that only runs against fixtures needs no
// connection. Env names are iterated in sorted order so error messages
// are stable regardless of map iteration order.
func (c *Config) validateEnvs() error {
	names := make([]string, 0, len(c.Env))
	for name := range c.Env {
		names = append(names, name)
	}
	sort.Strings(names)

	var defaults []string
	for _, name := range names {
		env := c.Env[name]
		if strings.TrimSpace(env.Connection) == "" {
			return fmt.Errorf("env %q missing required field: connection", name)
		}
		if env.Default {
			defaults = append(defaults, name)
		}
	}
	if len(defaults) > 1 {
		return fmt.Errorf("only one env may set default = true, got %d: %s", len(defaults), strings.Join(defaults, ", "))
	}
	return nil
}
