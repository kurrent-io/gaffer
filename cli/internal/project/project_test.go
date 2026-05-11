package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindRootFrom_InProjectDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gaffer.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	root := FindRootFrom(dir)
	if root != dir {
		t.Fatalf("expected %s, got %s", dir, root)
	}
}

func TestFindRootFrom_InSubdir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gaffer.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	subdir := filepath.Join(dir, "projections")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	root := FindRootFrom(subdir)
	if root != dir {
		t.Fatalf("expected %s, got %s", dir, root)
	}
}

func TestFindRootFrom_NotFound(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	root := FindRootFrom(nested)
	if root != "" {
		t.Fatalf("expected empty string, got %s", root)
	}
}

