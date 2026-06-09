package config

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ResolvedEnv is the outcome of resolving an environment: its name and
// its connection string as written in the config. The connection is
// raw - any ${VAR} interpolation happens downstream at connect time,
// not here.
type ResolvedEnv struct {
	Name       string
	Connection string
}

// ResolveEnv selects an environment by name, or the default when name
// is empty:
//
//   - name == "" → the env with default = true. Errors if no env is
//     marked default (caller must pass --env) or no envs are configured.
//   - name != "" → the env with that exact name. Errors if absent.
//
// validate() guarantees at most one env sets default = true, so the
// empty-name path is unambiguous. The available env names are listed
// (sorted) in error messages to orient the user.
func (c *Config) ResolveEnv(name string) (ResolvedEnv, error) {
	if len(c.Env) == 0 {
		return ResolvedEnv{}, errors.New("no environments configured in gaffer.toml; add an [env.<name>] block")
	}
	if name == "" {
		for _, n := range c.envNames() {
			if c.Env[n].Default {
				return ResolvedEnv{Name: n, Connection: c.Env[n].Connection}, nil
			}
		}
		return ResolvedEnv{}, fmt.Errorf(
			"no default environment in gaffer.toml; pass --env <name> (available: %s)",
			strings.Join(c.envNames(), ", "),
		)
	}
	e, ok := c.Env[name]
	if !ok {
		return ResolvedEnv{}, fmt.Errorf(
			"unknown environment %q (available: %s)",
			name, strings.Join(c.envNames(), ", "),
		)
	}
	return ResolvedEnv{Name: name, Connection: e.Connection}, nil
}

// DefaultEnvConnection returns the connection of the env marked
// default = true, or "" when there's no default env. Unlike ResolveEnv
// it never errors - it's for best-effort/loose callers (the LSP
// describe path) that only need to know whether a default live target
// exists, not to fail a command.
func (c *Config) DefaultEnvConnection() string {
	for _, n := range c.envNames() {
		if c.Env[n].Default {
			return c.Env[n].Connection
		}
	}
	return ""
}

// envNames returns the configured env names in sorted order.
func (c *Config) envNames() []string {
	names := make([]string, 0, len(c.Env))
	for n := range c.Env {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
