package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureGitignoreEntries_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")

	err := ensureGitignoreEntries(path, []string{".env", ".gaffer/"})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, ".env\n") {
		t.Error("expected .env entry")
	}
	if !strings.Contains(content, ".gaffer/\n") {
		t.Error("expected .gaffer/ entry")
	}
}

func TestEnsureGitignoreEntries_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")

	if err := os.WriteFile(path, []byte("node_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ensureGitignoreEntries(path, []string{".env", ".gaffer/"})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.Contains(content, "node_modules/\n") {
		t.Error("existing entries should be preserved")
	}
	if !strings.Contains(content, ".env\n") {
		t.Error("expected .env appended")
	}
}

func TestEnsureGitignoreEntries_AlreadyPresent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")

	initial := ".env\n.gaffer/\n"
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ensureGitignoreEntries(path, []string{".env", ".gaffer/"})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != initial {
		t.Error("file should not be modified when all entries present")
	}
}

func TestEnsureGitignoreEntries_NoTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")

	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ensureGitignoreEntries(path, []string{".env"})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	content := string(data)
	if !strings.HasPrefix(content, "existing\n") {
		t.Error("should add newline before appending")
	}
}
