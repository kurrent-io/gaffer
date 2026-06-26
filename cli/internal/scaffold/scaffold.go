package scaffold

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/pathutil"
	"github.com/kurrent-io/gaffer/cli/internal/project"
)

var supportedExtensions = []string{".js"}

// ListExtensions returns a copy of the allowlist for help-text rendering.
func ListExtensions() []string {
	return slices.Clone(supportedExtensions)
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
//
// engineVersion is written onto the new projection (engine_version is
// required per projection). Callers pass the user's choice, defaulting
// to config.DefaultEngineVersion.
func Scaffold(
	root string,
	cfg *config.Config,
	name, relPath, source, partition string,
	emit bool,
	engineVersion int,
) (*Result, error) {
	if engineVersion != 1 && engineVersion != 2 {
		return nil, fmt.Errorf("engine_version must be 1 or 2, got %d", engineVersion)
	}

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

	// Generate before any filesystem side effects so an invalid
	// source/partition combination fails without leaving a directory
	// behind.
	content, err := GenerateSource(source, partition, emit)
	if err != nil {
		return nil, err
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

	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("writing file: %w", err)
	}

	ev := engineVersion
	cfg.Projection = append(cfg.Projection, config.Projection{
		Name:          name,
		Entry:         cleanRel,
		EngineVersion: &ev,
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
	// Reject Windows drive-letter forms before any normalisation.
	// On non-Windows hosts filepath.IsAbs doesn't recognise them,
	// and after backslash normalisation path.IsAbs doesn't either,
	// so an LLM-supplied "C:\..." could otherwise scaffold into
	// `<root>/C:/...` on a Linux server.
	if pathutil.HasWindowsDrivePrefix(userInput) {
		return "", fmt.Errorf(
			"projection path %q must be relative to the project root",
			userInput,
		)
	}
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
	if pathutil.EscapesRoot(normalised) {
		return "", fmt.Errorf("projection path %q is outside the project root", userInput)
	}
	cleaned := path.Clean(normalised)
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

// assertUnderRoot verifies the resolved parent of absPath is still
// inside root, using pathutil's symlink-aware containment check so
// an in-tree symlink can't smuggle the write past the lexical
// no-escape check upstream.
func assertUnderRoot(root, absPath, userInput string) error {
	inside, err := pathutil.IsInsideRoot(root, filepath.Dir(absPath))
	if err != nil {
		return fmt.Errorf("resolving %s: %w", userInput, err)
	}
	if !inside {
		return fmt.Errorf("projection path %q is outside the project root", userInput)
	}
	return nil
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
		// foreachStream() partitions a multi-stream source; the runtime
		// rejects it on a single stream (fromStream). Catch it here so
		// scaffold can't emit a projection that only fails at run time.
		if strings.HasPrefix(source, "stream:") {
			return "", fmt.Errorf("per-stream partitioning is not supported with a single-stream source; use 'all' or 'category:<name>', or partition 'none'")
		}
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
