// Package pathutil holds path-handling primitives shared across the
// CLI surfaces. Path safety rules (no escape past a root, no Windows
// drive-letter forms) used to be inlined in each caller; subtle
// drift between those copies produced real bugs (e.g. config
// validation rejecting filenames that scaffold accepted). The
// canonical rule lives here so every caller picks up the same
// semantics.
package pathutil

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// HasWindowsDrivePrefix reports whether s starts with a Windows
// drive-letter prefix - "C:", "C:\foo", or the drive-relative
// "C:foo" form. Detection is host-OS-independent so a Linux server
// receiving a Windows-shaped path from an LLM still rejects it.
func HasWindowsDrivePrefix(s string) bool {
	if len(s) < 2 {
		return false
	}
	c := s[0]
	if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
		return false
	}
	return s[1] == ':'
}

// EscapesRoot reports whether a relative path resolves outside the
// project root. Only proper parent traversal counts; literal
// filenames like "..hidden.js" stay inside and must not be rejected.
//
// Operates on slash-form (forward slashes) regardless of host OS,
// since the canonical form in gaffer.toml is slash-only.
func EscapesRoot(rel string) bool {
	cleaned := path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	return cleaned == ".." || strings.HasPrefix(cleaned, "../")
}

// ResolveAncestorSymlinks walks up to the deepest existing ancestor
// of p, EvalSymlinks-resolves it, then rejoins the missing-suffix
// portion. Necessary because filepath.EvalSymlinks errors out on
// paths whose leaf doesn't exist yet (a common scaffold case: we're
// about to create the file).
//
// Catches symlink escape anywhere on the path - even when the
// immediate parent doesn't exist - by walking up until something
// real is found.
func ResolveAncestorSymlinks(p string) (string, error) {
	p = filepath.Clean(p)
	suffix := ""
	for {
		if _, err := os.Stat(p); err == nil {
			resolved, err := filepath.EvalSymlinks(p)
			if err != nil {
				return "", err
			}
			if suffix == "" {
				return resolved, nil
			}
			return filepath.Join(resolved, suffix), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(p)
		if parent == p {
			// Hit filesystem root without finding any existing
			// ancestor - return the lexical clean.
			return filepath.Join(p, suffix), nil
		}
		suffix = filepath.Join(filepath.Base(p), suffix)
		p = parent
	}
}

// IsInsideRoot reports whether abs (an absolute filesystem path)
// resolves to a location inside root, following any symlinks on
// either side. Used as a defence against in-tree symlinks pointing
// outside, paired with a lexical EscapesRoot check upstream.
//
// Returns (false, nil) when abs is genuinely outside; (false, err)
// when symlink resolution itself fails.
func IsInsideRoot(root, abs string) (bool, error) {
	resolvedRoot, err := ResolveAncestorSymlinks(root)
	if err != nil {
		return false, fmt.Errorf("resolving project root: %w", err)
	}
	resolvedAbs, err := ResolveAncestorSymlinks(abs)
	if err != nil {
		return false, fmt.Errorf("resolving %s: %w", abs, err)
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedAbs)
	if err != nil {
		return false, nil
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, nil
	}
	return true, nil
}
