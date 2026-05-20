package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestEmitsStructuredOutput_AlwaysStructured(t *testing.T) {
	for _, name := range []string{"manifest", "lsp", "mcp"} {
		t.Run(name, func(t *testing.T) {
			cmd := &cobra.Command{Use: name}
			if !emitsStructuredOutput(cmd) {
				t.Errorf("%s should be classified as structured", name)
			}
		})
	}
}

func TestEmitsStructuredOutput_HumanCommands(t *testing.T) {
	for _, name := range []string{"init", "scaffold", "version", "config"} {
		t.Run(name, func(t *testing.T) {
			cmd := &cobra.Command{Use: name}
			if emitsStructuredOutput(cmd) {
				t.Errorf("%s should not be classified as structured", name)
			}
		})
	}
}

// info / dev expose --json; suppression flips with that flag so the
// notice prints on the default (human) path and stays out of the
// JSON-piped path.
func TestEmitsStructuredOutput_JSONFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "info"}
	cmd.Flags().Bool("json", false, "")

	if emitsStructuredOutput(cmd) {
		t.Error("info without --json should not be structured")
	}

	if err := cmd.Flags().Set("json", "true"); err != nil {
		t.Fatalf("set --json: %v", err)
	}
	if !emitsStructuredOutput(cmd) {
		t.Error("info --json should be structured")
	}
}

// Commands without a --json flag are unaffected by the json-flag
// branch; the helper must not error or panic looking it up.
func TestEmitsStructuredOutput_NoJSONFlag(t *testing.T) {
	cmd := &cobra.Command{Use: "init"}
	if emitsStructuredOutput(cmd) {
		t.Error("command without --json flag should not be structured")
	}
}
