package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/kurrent-io/gaffer/cli/internal/project"
)

// InitProject creates a new gaffer.toml in dir with default settings
// (engine version 2) and returns the path written. The create is atomic
// (O_EXCL): if a manifest already exists, or two callers race, exactly
// one wins and the rest get the already-exists error - no truncating an
// existing file. Shared by `gaffer init` and the MCP init tool so the
// two can't drift on what a fresh project looks like.
func InitProject(dir string) (string, error) {
	configPath := project.ConfigPath(dir)

	data, err := Marshal(&Config{EngineVersion: 2})
	if err != nil {
		return "", err
	}

	f, err := os.OpenFile(configPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("gaffer.toml already exists in %s", dir)
		}
		return "", fmt.Errorf("creating gaffer.toml: %w", err)
	}

	if _, err := f.Write(data); err != nil {
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
