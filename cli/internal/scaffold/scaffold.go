package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

type Result struct {
	RelPath string
	Name    string
}

func Scaffold(root string, cfg *config.Config, name, source, partition string, emit bool) (*Result, error) {
	if cfg.FindProjection(name) != nil {
		return nil, fmt.Errorf("projection %q already exists in gaffer.toml", name)
	}

	relPath := filepath.Join("projections", name+".js")
	absPath := filepath.Join(root, relPath)

	if _, err := os.Stat(absPath); err == nil {
		return nil, fmt.Errorf("file already exists: %s", relPath)
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
		Entry: relPath,
	})

	configPath := filepath.Join(root, "gaffer.toml")
	if err := config.Save(configPath, cfg); err != nil {
		return nil, fmt.Errorf("updating gaffer.toml: %w", err)
	}

	return &Result{RelPath: relPath, Name: name}, nil
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
	sb.WriteString("    $init: function() {\n")
	sb.WriteString("      return {};\n")
	sb.WriteString("    },\n")
	sb.WriteString("    // Add your event handlers here\n")
	sb.WriteString("    // EventType: function(state, event) {\n")

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
