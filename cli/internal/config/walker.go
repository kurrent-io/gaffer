package config

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	gitignore "github.com/sabhiram/go-gitignore"
)

// Directories never descended into, regardless of .gitignore. Covers
// the common bloat dirs that workspaces often DON'T list in their
// .gitignore (because committing them is unthinkable) but a
// config-file walk has no business entering. Skipped at any depth
// below the root - the root itself is never skipped even if it
// happens to be named one of these.
var hardcodedSkipDirs = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	"vendor":       {},
}

// File basenames recognised as gaffer config. Hardcoded for V1; a
// future jsonc/kdl format would add an entry here in lockstep with
// each editor's activation manifest.
var configFileNames = []string{"gaffer.toml"}

// WalkConfigs walks `root` looking for gaffer config files and
// returns their absolute paths in lexicographic order. Honors a
// root-level .gitignore (single file - no per-directory
// accumulation) and a hardcoded skip list of standard noise dirs.
// Does not follow symlinks. Cancels promptly on ctx cancellation.
//
// Patterns matching the pulumi/wails/bearer single-root-.gitignore
// approach: nested .gitignore files are ignored. Acceptable for
// finding gaffer config files; pathological case (gaffer.toml only
// excluded by a nested .gitignore) is recovered when the user opens
// the file in the editor and the LSP server's didOpen path picks
// it up.
func WalkConfigs(ctx context.Context, root string) ([]string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	// Resolve a symlinked root to its target so WalkDir can
	// descend it. Unlike filepath.Walk, WalkDir doesn't follow
	// the root symlink itself; without resolving here, pointing
	// the LSP at a symlinked workspace root yields zero results.
	// Nested symlinks below the root are still skipped (see the
	// in-callback symlink check).
	//
	// Doubles as a missing-root error path: EvalSymlinks returns
	// a stat error for nonexistent paths, surfaced as-is rather
	// than swallowed by the per-entry walkErr handler below.
	rootAbs, err = filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return nil, err
	}

	matcher := loadRootGitignore(rootAbs)

	var found []string
	err = filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Permission errors, race-deleted entries, etc. Skip the
			// affected entry rather than failing the whole walk.
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// Root is never skipped, even when its basename matches the
		// hardcoded list or it's a symlink - the user pointed us
		// here deliberately. Checked before the symlink branch so
		// a symlinked workspace root still gets walked.
		if path == rootAbs {
			return nil
		}
		// Symlinks: don't follow. Avoids cycles + escape into
		// arbitrary parts of the filesystem. WalkDir already won't
		// descend a symlinked directory (it lstats), but the
		// callback still fires once for each symlink entry. For
		// files we need to skip explicitly so a `gaffer.toml`
		// symlink pointing outside the workspace isn't matched by
		// basename below.
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(rootAbs, path)
		if err != nil {
			return nil
		}
		// .gitignore patterns are forward-slash; on Windows
		// filepath.Rel returns backslashes which sabhiram's
		// MatchesPath would silently fail to match. Normalise.
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if _, skip := hardcodedSkipDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			// sabhiram's MatchesPath requires the trailing slash to
			// match a directory-only pattern like `build/`. Try both
			// so plain (`build`) and dir-only (`build/`) patterns
			// prune the descent.
			if matcher != nil && (matcher.MatchesPath(rel) || matcher.MatchesPath(rel+"/")) {
				return filepath.SkipDir
			}
			return nil
		}
		if matcher != nil && matcher.MatchesPath(rel) {
			return nil
		}
		for _, name := range configFileNames {
			if d.Name() == name {
				found = append(found, path)
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(found)
	return found, nil
}

// loadRootGitignore reads <root>/.gitignore if present and returns a
// compiled matcher. Returns nil if the file doesn't exist or can't
// be read; callers must handle nil as "no patterns to match."
func loadRootGitignore(root string) *gitignore.GitIgnore {
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return nil
	}
	return gitignore.CompileIgnoreLines(strings.Split(string(data), "\n")...)
}
