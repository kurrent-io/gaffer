package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config represents a gaffer.toml file.
type Config struct {
	Connection         string       `toml:"connection,omitempty"`
	EngineVersion      int          `toml:"engine_version,omitempty"`
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

// EffectiveEngineVersion returns the projection's engine_version, falling
// back to the top-level engine_version. Returns 0 if neither is set.
func (c *Config) EffectiveEngineVersion(p *Projection) int {
	if p.EngineVersion != 0 {
		return p.EngineVersion
	}
	return c.EngineVersion
}

// IsEnabled returns true if the projection is enabled (default true).
func (p Projection) IsEnabled() bool {
	if p.Enabled == nil {
		return true
	}
	return *p.Enabled
}

// Load reads and parses a gaffer.toml file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
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
	seen := make(map[string]bool)
	for _, p := range c.Projection {
		if p.Name == "" {
			return fmt.Errorf("projection missing required field: name")
		}
		if p.Entry == "" {
			return fmt.Errorf("projection %q missing required field: entry", p.Name)
		}
		if p.EngineVersion != 0 && p.EngineVersion != 1 && p.EngineVersion != 2 {
			return fmt.Errorf("projection %q engine_version must be 1 or 2, got %d", p.Name, p.EngineVersion)
		}
		if c.EffectiveEngineVersion(&p) == 0 {
			return fmt.Errorf("projection %q has no engine_version set (also missing top-level engine_version)", p.Name)
		}
		cleaned := filepath.Clean(p.Entry)
		if strings.HasPrefix(cleaned, "..") {
			return fmt.Errorf("projection %q entry must not escape project root: %s", p.Name, p.Entry)
		}
		if seen[p.Name] {
			return fmt.Errorf("duplicate projection name: %q", p.Name)
		}
		seen[p.Name] = true

		// Iterate in sorted order so error messages are stable.
		for _, name := range p.FixtureNames() {
			if name == "" {
				return fmt.Errorf("projection %q has a fixture with an empty name", p.Name)
			}
			path := p.Fixtures[name]
			if path == "" {
				return fmt.Errorf("projection %q fixture %q has empty path", p.Name, name)
			}
			if strings.HasPrefix(filepath.Clean(path), "..") {
				return fmt.Errorf("projection %q fixture %q path must not escape project root: %s", p.Name, name, path)
			}
		}
	}
	return nil
}
