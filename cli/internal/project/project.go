package project

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/kurrent-io/gaffer/cli/internal/pathutil"
)

var ErrNotInProject = errors.New("not in a gaffer project (no gaffer.toml found)")

// ConfigFileName is the canonical project-config filename. Exported so
// other packages (notably cli/internal/telemetry, which walks for the
// same file under its own bound-aware policy) can refer to it without
// hardcoding the string.
const ConfigFileName = "gaffer.toml"

// ConfigPath returns the canonical project-config path for a given
// project root (`<root>/gaffer.toml`). Use it instead of inlining
// filepath.Join(root, "gaffer.toml") so the filename's one source of
// truth stays at ConfigFileName.
func ConfigPath(root string) string {
	return filepath.Join(root, ConfigFileName)
}

// FindRoot walks up from the current directory looking for gaffer.toml.
// Returns the directory containing it, or empty string if not found.
func FindRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return FindRootFrom(dir)
}

// FindRootFrom walks up from the given directory looking for gaffer.toml.
// Returns the directory containing it, or empty string if not found.
func FindRootFrom(dir string) string {
	return pathutil.WalkUpFor(dir, ConfigFileName, "")
}
