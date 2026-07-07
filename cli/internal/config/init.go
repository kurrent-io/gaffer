package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/kurrent-io/gaffer/cli/internal/project"
)

// DefaultEngineVersion is the engine_version scaffold writes onto a new
// projection when the user doesn't choose otherwise. v2 is the nudge;
// v1 has to be asked for explicitly.
const DefaultEngineVersion = 2

// initTemplate is the starter gaffer.toml. Everything is commented out:
// a fresh project has no active environments or projections, so nothing
// tries to dial a placeholder connection, but the [env.*] and
// [[projection]] shapes are documented inline so the file doubles as a
// quick reference. The toml encoder can't emit comments, hence a
// literal template rather than Marshal of a Config.
const initTemplate = `# gaffer.toml - projection toolkit config.
# Docs: https://gaffer.kurrent.io

# Environments are the targets you deploy and inspect projections against.
# Each [env.<name>] is self-contained and carries its own connection.
# Mark exactly one as default; it is used when --env is omitted.
# ${VAR} in a connection is resolved from the process environment and
# .env / .env.<name> files, so no credentials need to be committed.
# production = true opts an env into the production guard tier (louder
# confirmations, --no-validate refused).
#
# [env.local]
# connection = "esdb://localhost:2113?tls=false"
# default = true
#
# [env.prod]
# connection = "${KURRENT_PROD_CONNECTION}"
# production = true

# Add projections with 'gaffer scaffold', or by hand. engine_version is
# required on each projection (use 2 unless you need the v1 engine).
#
# [[projection]]
# name = "order-count"
# entry = "projections/order-count.js"
# engine_version = 2
`

// InitProject creates a new gaffer.toml in dir from the starter
// template and returns the path written. The create is atomic (O_EXCL):
// if a manifest already exists, or two callers race, exactly one wins
// and the rest get the already-exists error - no truncating an existing
// file. Shared by `gaffer init` and the MCP init tool so the two can't
// drift on what a fresh project looks like.
func InitProject(dir string) (string, error) {
	configPath := project.ConfigPath(dir)

	f, err := os.OpenFile(configPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("gaffer.toml already exists in %s", dir)
		}
		return "", fmt.Errorf("creating gaffer.toml: %w", err)
	}

	if _, err := f.Write([]byte(initTemplate)); err != nil {
		_ = f.Close()
		_ = os.Remove(configPath)
		return "", fmt.Errorf("writing gaffer.toml: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(configPath)
		return "", fmt.Errorf("writing gaffer.toml: %w", err)
	}
	return configPath, nil
}
