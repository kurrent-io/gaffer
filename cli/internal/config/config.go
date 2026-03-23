package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config represents a gaffer.toml file.
type Config struct {
	Projection []Projection `toml:"projection"`
}

// Projection is a single projection entry in the config.
type Projection struct {
	Name    string `toml:"name"`
	Entry   string `toml:"entry"`
	Engine  string `toml:"engine,omitempty"`
	Enabled *bool  `toml:"enabled,omitempty"`
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
	seen := make(map[string]bool)
	for _, p := range c.Projection {
		if p.Name == "" {
			return fmt.Errorf("projection missing required field: name")
		}
		if p.Entry == "" {
			return fmt.Errorf("projection %q missing required field: entry", p.Name)
		}
		cleaned := filepath.Clean(p.Entry)
		if strings.HasPrefix(cleaned, "..") {
			return fmt.Errorf("projection %q entry must not escape project root: %s", p.Name, p.Entry)
		}
		if seen[p.Name] {
			return fmt.Errorf("duplicate projection name: %q", p.Name)
		}
		seen[p.Name] = true
	}
	return nil
}
