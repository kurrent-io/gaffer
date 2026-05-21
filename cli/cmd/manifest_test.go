package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func manifestCommands(t *testing.T) map[string]manifestCommand {
	t.Helper()
	return buildManifest(NewRootCmd(), "test", "").Commands
}

func commandFlags(t *testing.T, commands map[string]manifestCommand, name string) map[string]bool {
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

func TestBuildManifest_IncludesNestedCommands(t *testing.T) {
	commands := manifestCommands(t)

	expected := []string{
		"config telemetry status",
		"config telemetry on",
		"config telemetry off",
	}
	for _, name := range expected {
		if _, ok := commands[name]; !ok {
			t.Errorf("expected nested command %q in manifest", name)
		}
	}
}

// Group commands like `config` and `config telemetry` are navigation
// nodes - not directly invocable - so the manifest, which lists
// invocable commands, must not include them as bare entries.
func TestBuildManifest_ExcludesNonRunnableGroups(t *testing.T) {
	commands := manifestCommands(t)

	for _, name := range []string{"config", "config telemetry"} {
		if _, ok := commands[name]; ok {
			t.Errorf("group %q should not be in manifest", name)
		}
	}
}

func TestManifestCmd_Hidden(t *testing.T) {
	if !newManifestCmd().Hidden {
		t.Error("manifest command should be Hidden")
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
	m := buildManifest(NewRootCmd(), "1.0.0", "")

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}

	var parsed manifest
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

// updateAvailable is emitted as JSON null when no upgrade is known
// (rather than omitted) so VS Code-side consumers can branch on the
// field unconditionally rather than checking for presence.
func TestBuildManifest_UpdateAvailable_NullWhenEmpty(t *testing.T) {
	m := buildManifest(NewRootCmd(), "1.0.0", "")
	if m.UpdateAvailable != nil {
		t.Errorf("expected nil UpdateAvailable, got %q", *m.UpdateAvailable)
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); !strings.Contains(got, `"updateAvailable":null`) {
		t.Errorf("expected updateAvailable:null in JSON, got %s", got)
	}
}

func TestBuildManifest_UpdateAvailable_SetWhenProvided(t *testing.T) {
	m := buildManifest(NewRootCmd(), "1.0.0", "1.2.3")
	if m.UpdateAvailable == nil {
		t.Fatal("expected UpdateAvailable to be set")
	}
	if got := *m.UpdateAvailable; got != "1.2.3" {
		t.Errorf("expected UpdateAvailable=1.2.3, got %q", got)
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); !strings.Contains(got, `"updateAvailable":"1.2.3"`) {
		t.Errorf("expected updateAvailable:\"1.2.3\" in JSON, got %s", got)
	}
}
