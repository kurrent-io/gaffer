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
	return FindRootFromBounded(dir, "")
}

// FindRootFromBounded is FindRootFrom with an upper bound: the walk stops
// before inspecting stopAt, so a stray gaffer.toml at or above stopAt (e.g.
// $HOME, or a world-writable /tmp on a shared host) is never treated as the
// project root. An empty stopAt is unbounded (walks to the filesystem root).
// Used for the startup .env auto-load, which would otherwise pull a stray
// ancestor's secrets into every invocation.
func FindRootFromBounded(dir, stopAt string) string {
	return pathutil.WalkUpFor(dir, ConfigFileName, stopAt)
}
