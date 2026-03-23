package env

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_NoEnvFile(t *testing.T) {
	dir := t.TempDir()
	err := Load(dir, "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestLoad_BaseEnvFile(t *testing.T) {
	dir := t.TempDir()
	envContent := "GAFFER_CONNECTION=esdb://localhost:2113\nTEST_VAR=hello\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(envContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Clean up env vars after test
	t.Cleanup(func() {
		_ = os.Unsetenv("GAFFER_CONNECTION")
		_ = os.Unsetenv("TEST_VAR")
	})

	if err := Load(dir, ""); err != nil {
		t.Fatal(err)
	}

	if Connection() != "esdb://localhost:2113" {
		t.Fatalf("expected connection string, got %q", Connection())
	}

	if os.Getenv("TEST_VAR") != "hello" {
		t.Fatalf("expected TEST_VAR=hello, got %q", os.Getenv("TEST_VAR"))
	}
}

func TestLoad_OverrideEnvFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("GAFFER_CONNECTION=esdb://localhost:2113\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env.prod"), []byte("GAFFER_CONNECTION=esdb://prod:2113\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Unsetenv("GAFFER_CONNECTION") })

	if err := Load(dir, "prod"); err != nil {
		t.Fatal(err)
	}

	if Connection() != "esdb://prod:2113" {
		t.Fatalf("expected prod connection, got %q", Connection())
	}
}

func TestLoad_OverrideFileMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("GAFFER_CONNECTION=esdb://localhost:2113\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Unsetenv("GAFFER_CONNECTION") })

	// Loading with a nonexistent override should still load base
	if err := Load(dir, "staging"); err != nil {
		t.Fatal(err)
	}

	if Connection() != "esdb://localhost:2113" {
		t.Fatalf("expected base connection, got %q", Connection())
	}
}

func TestConnection_Empty(t *testing.T) {
	_ = os.Unsetenv("GAFFER_CONNECTION")
	if Connection() != "" {
		t.Fatalf("expected empty, got %q", Connection())
	}
}
