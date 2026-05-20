package cmd

import (
	"encoding/json"
	"testing"
)

func TestBuildCommandManifest_IncludesExpectedCommands(t *testing.T) {
	commands := buildCommandManifest(NewRootCmd())

	expected := []string{"init", "scaffold", "dev", "info", "mcp"}
	for _, name := range expected {
		if _, ok := commands[name]; !ok {
			t.Errorf("expected command %q in manifest", name)
		}
	}
}

func TestBuildCommandManifest_ExcludesInternalCommands(t *testing.T) {
	commands := buildCommandManifest(NewRootCmd())

	excluded := []string{"manifest", "help", "version", "completion"}
	for _, name := range excluded {
		if _, ok := commands[name]; ok {
			t.Errorf("command %q should not be in manifest", name)
		}
	}
}

func TestBuildCommandManifest_DevFlags(t *testing.T) {
	commands := buildCommandManifest(NewRootCmd())

	dev, ok := commands["dev"]
	if !ok {
		t.Fatal("expected dev command")
	}

	expected := []string{"events", "json", "connection", "debug", "debug-port"}
	flagSet := make(map[string]bool)
	for _, f := range dev.Flags {
		flagSet[f] = true
	}

	for _, name := range expected {
		if !flagSet[name] {
			t.Errorf("expected flag %q on dev command", name)
		}
	}
}

func TestBuildCommandManifest_ScaffoldFlags(t *testing.T) {
	commands := buildCommandManifest(NewRootCmd())

	scaffold, ok := commands["scaffold"]
	if !ok {
		t.Fatal("expected scaffold command")
	}

	expected := []string{"name", "source", "partition", "emit"}
	flagSet := make(map[string]bool)
	for _, f := range scaffold.Flags {
		flagSet[f] = true
	}

	for _, name := range expected {
		if !flagSet[name] {
			t.Errorf("expected flag %q on scaffold command", name)
		}
	}
}

func TestBuildCommandManifest_InitFlags(t *testing.T) {
	commands := buildCommandManifest(NewRootCmd())

	init, ok := commands["init"]
	if !ok {
		t.Fatal("expected init command")
	}

	flagSet := make(map[string]bool)
	for _, f := range init.Flags {
		flagSet[f] = true
	}

	if !flagSet["yes"] {
		t.Error("expected flag \"yes\" on init command")
	}
}

func TestBuildCommandManifest_InfoFlags(t *testing.T) {
	commands := buildCommandManifest(NewRootCmd())

	info, ok := commands["info"]
	if !ok {
		t.Fatal("expected info command")
	}

	flagSet := make(map[string]bool)
	for _, f := range info.Flags {
		flagSet[f] = true
	}

	if !flagSet["json"] {
		t.Error("expected flag \"json\" on info command")
	}
}

func TestManifestJSON(t *testing.T) {
	m := manifest{
		Version:  "1.0.0",
		Commands: buildCommandManifest(NewRootCmd()),
	}

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
