package dotenv

import (
	"os"
	"path/filepath"
	"testing"
)

// clearEnv unsets the KurrentDB credential vars and registers cleanup
// to restore their original values after the test.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"KURRENTDB_USERNAME", "KURRENTDB_PASSWORD"} {
		orig, set := os.LookupEnv(key)
		_ = os.Unsetenv(key)
		if set {
			t.Cleanup(func() { _ = os.Setenv(key, orig) })
		} else {
			t.Cleanup(func() { _ = os.Unsetenv(key) })
		}
	}
}

func TestLoad_NoEnvFile(t *testing.T) {
	dir := t.TempDir()
	err := Load(dir, "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestLoad_BaseEnvFile(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("KURRENTDB_USERNAME=admin\nKURRENTDB_PASSWORD=changeit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Load(dir, ""); err != nil {
		t.Fatal(err)
	}

	user, pass := Credentials()
	if user != "admin" {
		t.Fatalf("expected username admin, got %q", user)
	}
	if pass != "changeit" {
		t.Fatalf("expected password changeit, got %q", pass)
	}
}

func TestLoad_OverrideEnvFile(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("KURRENTDB_USERNAME=admin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env.prod"), []byte("KURRENTDB_USERNAME=produser\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Load(dir, "prod"); err != nil {
		t.Fatal(err)
	}

	user, _ := Credentials()
	if user != "produser" {
		t.Fatalf("expected produser, got %q", user)
	}
}

func TestLoad_OverrideFileMissing(t *testing.T) {
	clearEnv(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("KURRENTDB_USERNAME=admin\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Load(dir, "staging"); err != nil {
		t.Fatal(err)
	}

	user, _ := Credentials()
	if user != "admin" {
		t.Fatalf("expected admin, got %q", user)
	}
}

func TestCredentials_Empty(t *testing.T) {
	clearEnv(t)

	user, pass := Credentials()
	if user != "" || pass != "" {
		t.Fatalf("expected empty credentials, got %q/%q", user, pass)
	}
}
