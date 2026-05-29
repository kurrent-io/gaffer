package config

import (
	"fmt"
	"os"

	"github.com/kurrent-io/gaffer/cli/internal/project"
)

// InitProject creates a new gaffer.toml in dir with default settings
// (engine version 2) and returns the path written. It errors if a
// manifest already exists in dir. Shared by `gaffer init` and the MCP
// init tool so the two can't drift on what a fresh project looks like.
func InitProject(dir string) (string, error) {
	configPath := project.ConfigPath(dir)
	if _, err := os.Stat(configPath); err == nil {
		return "", fmt.Errorf("gaffer.toml already exists in %s", dir)
	}
	if err := Save(configPath, &Config{EngineVersion: 2}); err != nil {
		return "", err
	}
	return configPath, nil
}
