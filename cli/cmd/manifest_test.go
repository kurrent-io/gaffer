package cmd

import (
	"encoding/json"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
)

func manifestCommands(t *testing.T) map[string]cliout.ManifestCommand {
	t.Helper()
	return cliout.BuildManifest(NewRootCmd(), "test").Commands
}

func commandFlags(t *testing.T, commands map[string]cliout.ManifestCommand, name string) map[string]bool {
	t.Helper()
	cmd, ok := commands[name]
	if !ok {
		t.Fatalf("expected command %q", name)
	}
	flagSet := make(map[string]bool, len(cmd.Flags))
	for _, f := range cmd.Flags {
		flagSet[f] = true
	}
	return flagSet
}

func TestBuildManifest_IncludesExpectedCommands(t *testing.T) {
	commands := manifestCommands(t)

	expected := []string{"init", "scaffold", "dev", "info", "mcp"}
	for _, name := range expected {
		if _, ok := commands[name]; !ok {
			t.Errorf("expected command %q in manifest", name)
		}
	}
}

func TestBuildManifest_ExcludesInternalCommands(t *testing.T) {
	commands := manifestCommands(t)

	excluded := []string{"manifest", "help", "version", "completion"}
	for _, name := range excluded {
		if _, ok := commands[name]; ok {
			t.Errorf("command %q should not be in manifest", name)
		}
	}
}

func TestBuildManifest_DevFlags(t *testing.T) {
	flags := commandFlags(t, manifestCommands(t), "dev")
	for _, name := range []string{"events", "json", "connection", "debug", "debug-port"} {
		if !flags[name] {
			t.Errorf("expected flag %q on dev command", name)
		}
	}
}

func TestBuildManifest_ScaffoldFlags(t *testing.T) {
	flags := commandFlags(t, manifestCommands(t), "scaffold")
	for _, name := range []string{"name", "source", "partition", "emit"} {
		if !flags[name] {
			t.Errorf("expected flag %q on scaffold command", name)
		}
	}
}

func TestBuildManifest_InitFlags(t *testing.T) {
	flags := commandFlags(t, manifestCommands(t), "init")
	if !flags["yes"] {
		t.Error("expected flag \"yes\" on init command")
	}
}

func TestBuildManifest_InfoFlags(t *testing.T) {
	flags := commandFlags(t, manifestCommands(t), "info")
	if !flags["json"] {
		t.Error("expected flag \"json\" on info command")
	}
}

func TestBuildManifest_JSONShape(t *testing.T) {
	m := cliout.BuildManifest(NewRootCmd(), "1.0.0")

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}

	var parsed cliout.Manifest
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}

	if parsed.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", parsed.Version)
	}
	if len(parsed.Commands) == 0 {
		t.Error("expected commands in manifest")
	}
}
