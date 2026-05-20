package scaffold

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/project"
)

var supportedExtensions = []string{".js"}

// ListExtensions returns a copy of the allowlist for help-text rendering.
func ListExtensions() []string {
	out := make([]string, len(supportedExtensions))
	copy(out, supportedExtensions)
	return out
}

// IsSupported reports whether ext is in the allowlist.
func IsSupported(ext string) bool {
	for _, e := range supportedExtensions {
		if ext == e {
			return true
		}
	}
	return false
}

type Result struct {
	RelPath string
	Name    string
}

// Scaffold creates a projection file at relPath (interpreted relative
// to root) and registers it in gaffer.toml. The single point of
// validation: extension allowlist, no escape past root including via
// symlinks, no separator-flavour bypass.
//
// relPath may use `/` or `\` separators; the toml entry is stored
// slash-form regardless. If name is empty, it defaults to the file's
// basename without extension - kept here so the rule stays consistent
// across the CLI and MCP surfaces.
func Scaffold(
	root string,
	cfg *config.Config,
	name, relPath, source, partition string,
	emit bool,
) (*Result, error) {
	cleanRel, err := validateRelPath(relPath)
	if err != nil {
		return nil, err
	}

	if name == "" {
		name = strings.TrimSuffix(path.Base(cleanRel), path.Ext(cleanRel))
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("projection name is required")
	}
	if cfg.FindProjection(name) != nil {
		return nil, fmt.Errorf("projection %q already exists in gaffer.toml", name)
	}

	absPath := filepath.Join(root, filepath.FromSlash(cleanRel))

	// Resolve symlinks anywhere on the parent path so an in-tree
	// symlink pointing outside (e.g. `bad -> /etc`) can't smuggle
	// the write past the lexical no-escape check. There's a small
	// TOCTOU window between this check and MkdirAll/WriteFile - an
	// attacker with project-tree write access could swap a directory
	// for a symlink in between. In our threat model the attacker
	// already has write access, so the residual risk is acceptable.
	if err := assertUnderRoot(root, absPath, relPath); err != nil {
		return nil, err
	}

	if _, err := os.Stat(absPath); err == nil {
		return nil, fmt.Errorf("file already exists: %s", cleanRel)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating directory: %w", err)
	}

	content, err := GenerateSource(source, partition, emit)
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("writing file: %w", err)
	}

	cfg.Projection = append(cfg.Projection, config.Projection{
		Name:  name,
		Entry: cleanRel,
	})

	configPath := project.ConfigPath(root)
	if err := config.Save(configPath, cfg); err != nil {
		return nil, fmt.Errorf("updating gaffer.toml: %w", err)
	}

	return &Result{RelPath: cleanRel, Name: name}, nil
}

// validateRelPath enforces relative, supported-extension,
// non-escaping, non-empty-stem. Normalises `\` to `/` so Windows
// users can type either separator and the result is always
// slash-form (the canonical form stored in gaffer.toml). The
// `userInput` argument flows into error messages so the user sees
// what they actually typed, not the normalised form.
func validateRelPath(userInput string) (string, error) {
	if strings.TrimSpace(userInput) == "" {
		return "", fmt.Errorf("projection path is required")
	}
	// Reject Windows drive-letter forms (`C:\foo.js`, `C:foo.js`,
	// `C:/foo.js`) explicitly. On non-Windows hosts filepath.IsAbs
	// doesn't recognise them, and after backslash normalisation
	// path.IsAbs doesn't either - so without this guard an LLM
	// trained on Windows paths could scaffold into `<root>/C:/...`
	// on a Linux server.
	if hasWindowsDrivePrefix(userInput) {
		return "", fmt.Errorf(
			"projection path %q must be relative to the project root",
			userInput,
		)
	}
	// filepath.IsAbs is platform-aware on whatever we're running on.
	if filepath.IsAbs(userInput) {
		return "", fmt.Errorf(
			"projection path %q must be relative to the project root",
			userInput,
		)
	}
	normalised := strings.ReplaceAll(userInput, "\\", "/")
	if path.IsAbs(normalised) {
		return "", fmt.Errorf(
			"projection path %q must be relative to the project root",
			userInput,
		)
	}
	cleaned := path.Clean(normalised)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("projection path %q is outside the project root", userInput)
	}
	ext := path.Ext(cleaned)
	if !IsSupported(ext) {
		return "", fmt.Errorf(
			"projection path %q must end in one of %s",
			userInput,
			strings.Join(supportedExtensions, ", "),
		)
	}
	// Reject paths whose basename is only the extension (e.g. ".js"
	// or "foo/.js") - the toml key derivation would yield empty and
	// the user would see a confusing "name is required" downstream.
	if strings.TrimSuffix(path.Base(cleaned), ext) == "" {
		return "", fmt.Errorf("projection path %q is missing a file name", userInput)
	}
	return cleaned, nil
}

// assertUnderRoot resolves any symlinks on the parent path and
// verifies the resolved location is still inside root. Used after
// lexical validation so an in-tree symlink can't smuggle the write
// outside the project.
func assertUnderRoot(root, absPath, userInput string) error {
	resolvedDir, err := ResolveAncestorSymlinks(filepath.Dir(absPath))
	if err != nil {
		return fmt.Errorf("resolving %s: %w", userInput, err)
	}
	resolvedRoot, err := ResolveAncestorSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolving project root: %w", err)
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedDir)
	if err != nil {
		return fmt.Errorf("projection path %q is outside the project root", userInput)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("projection path %q is outside the project root", userInput)
	}
	return nil
}

// hasWindowsDrivePrefix matches `C:...` and `C:\...` / `C:/...`
// regardless of host OS. Covers `C:foo.js` (drive-relative) too -
// nothing about it should appear in a project-relative path.
func hasWindowsDrivePrefix(s string) bool {
	if len(s) < 2 {
		return false
	}
	c := s[0]
	if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
		return false
	}
	return s[1] == ':'
}

// ResolveAncestorSymlinks walks up to the deepest existing ancestor,
// resolves its symlinks with filepath.EvalSymlinks, then rejoins the
// missing-suffix portion. This catches escape via a symlink anywhere
// on the path - including when the immediate parent doesn't exist
// yet (scaffold creates it). A plain EvalSymlinks on a not-yet-
// existing dir returns ENOENT and would force a lexical-only
// fallback that misses the escape.
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
			// ancestor - return the lexical clean. (No symlink can
			// possibly be hiding here.)
			return filepath.Join(p, suffix), nil
		}
		suffix = filepath.Join(filepath.Base(p), suffix)
		p = parent
	}
}

func GenerateSource(source, partition string, emit bool) (string, error) {
	var sb strings.Builder

	switch {
	case strings.HasPrefix(source, "stream:"):
		name := escapeJS(strings.TrimPrefix(source, "stream:"))
		fmt.Fprintf(&sb, "fromStream('%s')\n", name)
	case strings.HasPrefix(source, "category:"):
		name := escapeJS(strings.TrimPrefix(source, "category:"))
		fmt.Fprintf(&sb, "fromCategory('%s')\n", name)
	case source == "all":
		sb.WriteString("fromAll()\n")
	default:
		return "", fmt.Errorf("unsupported source: %q (use 'all', 'stream:name', or 'category:name')", source)
	}

	switch partition {
	case "per-stream":
		sb.WriteString("  .foreachStream()\n")
	case "none":
		// no partitioning
	default:
		return "", fmt.Errorf("unsupported partition: %q (use 'none' or 'per-stream')", partition)
	}

	sb.WriteString("  .when({\n")
	sb.WriteString("    $init() {\n")
	sb.WriteString("      return {};\n")
	sb.WriteString("    },\n")
	sb.WriteString("    // Add your event handlers here\n")
	sb.WriteString("    // EventType(state, event) {\n")

	if emit {
		sb.WriteString("    //   emit('stream-name', 'EmittedType', { data: event.data });\n")
	}

	sb.WriteString("    //   return state;\n")
	sb.WriteString("    // }\n")
	sb.WriteString("  })\n")

	return sb.String(), nil
}

func escapeJS(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}
