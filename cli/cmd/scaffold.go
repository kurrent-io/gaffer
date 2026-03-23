package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/spf13/cobra"
)

var scaffoldCmd = &cobra.Command{
	Use:   "scaffold [name]",
	Short: "Add a new projection to the project",
	Args:  cobra.ExactArgs(1),
	RunE:  runScaffold,
}

var (
	scaffoldLang      string
	scaffoldSource    string
	scaffoldPartition string
	scaffoldEmit      bool
)

func init() {
	scaffoldCmd.Flags().StringVar(&scaffoldLang, "lang", "js", "Language (js)")
	scaffoldCmd.Flags().StringVar(&scaffoldSource, "source", "all", "Event source (all, stream:name, category:name)")
	scaffoldCmd.Flags().StringVar(&scaffoldPartition, "partition", "none", "Partitioning (none, per-stream)")
	scaffoldCmd.Flags().BoolVar(&scaffoldEmit, "emit", false, "Enable emit/linkTo")
}

func runScaffold(cmd *cobra.Command, args []string) error {
	name := args[0]

	root := project.FindRoot()
	if root == "" {
		return fmt.Errorf("not in a gaffer project (no gaffer.toml found)")
	}

	configPath := filepath.Join(root, "gaffer.toml")

	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	if cfg.FindProjection(name) != nil {
		return fmt.Errorf("projection %q already exists", name)
	}

	ext := langExtension(scaffoldLang)
	relPath := filepath.Join("projections", name+ext)
	absPath := filepath.Join(root, relPath)

	if _, err := os.Stat(absPath); err == nil {
		return fmt.Errorf("file already exists: %s", relPath)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return err
	}

	source, err := generateProjectionSource(scaffoldSource, scaffoldPartition, scaffoldEmit)
	if err != nil {
		return err
	}

	if err := os.WriteFile(absPath, []byte(source), 0o644); err != nil {
		return err
	}

	cfg.Projection = append(cfg.Projection, config.Projection{
		Name:  name,
		Entry: relPath,
	})

	if err := config.Save(configPath, cfg); err != nil {
		return err
	}

	fmt.Printf("Created %s\n", relPath)
	return nil
}

func langExtension(lang string) string {
	switch lang {
	case "ts":
		return ".ts"
	default:
		return ".js"
	}
}

func escapeJSString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}

func generateProjectionSource(source, partition string, emit bool) (string, error) {
	var sb strings.Builder

	switch {
	case strings.HasPrefix(source, "stream:"):
		name := escapeJSString(strings.TrimPrefix(source, "stream:"))
		fmt.Fprintf(&sb, "fromStream('%s')\n", name)
	case strings.HasPrefix(source, "category:"):
		name := escapeJSString(strings.TrimPrefix(source, "category:"))
		fmt.Fprintf(&sb, "fromCategory('%s')\n", name)
	default:
		sb.WriteString("fromAll()\n")
	}

	switch partition {
	case "per-stream":
		sb.WriteString("  .foreachStream()\n")
	case "none":
		// no partitioning
	default:
		return "", fmt.Errorf("unsupported partition mode: %q (use 'none' or 'per-stream')", partition)
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
