package project

import (
	"errors"
	"os"
	"path/filepath"
)

var ErrNotInProject = errors.New("not in a gaffer project (no gaffer.toml found)")

const configFileName = "gaffer.toml"

// FindRoot walks up from the current directory looking for gaffer.toml.
// Returns the directory containing it, or empty string if not found.
func FindRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, configFileName)); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
