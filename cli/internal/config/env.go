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
	OAuth      *OAuthConfig
	Cert       *CertAuth
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
		for _, n := range c.EnvNames() {
			if c.Env[n].Default {
				return c.Env[n].resolved(n), nil
			}
		}
		return ResolvedEnv{}, fmt.Errorf(
			"no default environment in gaffer.toml; pass --env <name> (available: %s)",
			strings.Join(c.EnvNames(), ", "),
		)
	}
	e, ok := c.Env[name]
	if !ok {
		return ResolvedEnv{}, fmt.Errorf(
			"unknown environment %q (available: %s)",
			name, strings.Join(c.EnvNames(), ", "),
		)
	}
	return e.resolved(name), nil
}

// resolved builds the ResolvedEnv view of an env under the given name.
func (e Env) resolved(name string) ResolvedEnv {
	return ResolvedEnv{
		Name:       name,
		Connection: e.Connection,
		OAuth:      e.OAuth,
		Cert:       e.certAuth(),
	}
}

// DefaultEnv returns the env marked default = true and true, or the
// zero ResolvedEnv and false when no env is the default. Unlike
// ResolveEnv it never errors - it's for callers that treat "no default"
// as a benign absence (dev falling back to fixtures, the loose LSP
// describe path) rather than a command failure.
func (c *Config) DefaultEnv() (ResolvedEnv, bool) {
	for _, n := range c.EnvNames() {
		if c.Env[n].Default {
			return c.Env[n].resolved(n), true
		}
	}
	return ResolvedEnv{}, false
}

// DefaultEnvConnection returns the default env's connection, or "" when
// there's no default env.
func (c *Config) DefaultEnvConnection() string {
	env, _ := c.DefaultEnv()
	return env.Connection
}

// EnvNames returns the configured env names in sorted order.
func (c *Config) EnvNames() []string {
	names := make([]string, 0, len(c.Env))
	for n := range c.Env {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
