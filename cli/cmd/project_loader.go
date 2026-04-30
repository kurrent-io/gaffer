package cmd

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/spf13/cobra"
)

func handleSessionError(cmd *cobra.Command, err error) error {
	if projErr, ok := err.(gafferruntime.ProjectionError); ok {
		r := lipgloss.NewRenderer(os.Stderr)
		errStyle := r.NewStyle().Foreground(lipgloss.Color("9"))
		_, _ = fmt.Fprintf(os.Stderr, "\n%s\n%s\n", errStyle.Render(projErr.ErrorCode()), projErr.Error())
		return silent(err)
	}
	return fmt.Errorf("failed to create projection session: %w", err)
}
