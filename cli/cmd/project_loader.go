package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/spf13/cobra"
)

type projectionContext struct {
	Root   string
	Config *config.Config
	Proj   *config.Projection
	Source string
	Engine string
}

func loadProjection(name string) (*projectionContext, error) {
	root := project.FindRoot()
	if root == "" {
		return nil, fmt.Errorf("not in a gaffer project (no gaffer.toml found)")
	}

	cfg, err := config.Load(filepath.Join(root, "gaffer.toml"))
	if err != nil {
		return nil, err
	}

	proj := cfg.FindProjection(name)
	if proj == nil {
		return nil, fmt.Errorf("projection %q not found in gaffer.toml", name)
	}

	source, err := os.ReadFile(filepath.Join(root, proj.Entry))
	if err != nil {
		return nil, fmt.Errorf("reading projection source: %w", err)
	}

	return &projectionContext{
		Root:   root,
		Config: cfg,
		Proj:   proj,
		Source: string(source),
		Engine: proj.EffectiveEngine(),
	}, nil
}

func handleSessionError(cmd *cobra.Command, err error) error {
	if projErr, ok := err.(gafferruntime.ProjectionError); ok {
		r := lipgloss.NewRenderer(os.Stderr)
		errStyle := r.NewStyle().Foreground(lipgloss.Color("9"))
		_, _ = fmt.Fprintf(os.Stderr, "\n%s\n%s\n", errStyle.Render(projErr.ErrorCode()), projErr.Error())
		cmd.SilenceErrors = true
		return err
	}
	return fmt.Errorf("failed to create projection session: %w", err)
}
