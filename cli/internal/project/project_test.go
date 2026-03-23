package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindRootInProjectDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gaffer.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	root := FindRoot()
	if root != dir {
		t.Fatalf("expected %s, got %s", dir, root)
	}
}

func TestFindRootInSubdir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gaffer.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	subdir := filepath.Join(dir, "projections")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()

	if err := os.Chdir(subdir); err != nil {
		t.Fatal(err)
	}

	root := FindRoot()
	if root != dir {
		t.Fatalf("expected %s, got %s", dir, root)
	}
}

func TestFindRootNotFound(t *testing.T) {
	// Create a deeply nested temp dir unlikely to have gaffer.toml above it
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()

	if err := os.Chdir(nested); err != nil {
		t.Fatal(err)
	}

	root := FindRoot()
	// Should not find gaffer.toml in the temp dir hierarchy
	if root != "" && !strings.HasPrefix(root, dir) {
		// Found one outside our temp dir - dirty environment, skip
		t.Skipf("found gaffer.toml outside test dir at %s (dirty environment)", root)
	}
	if root != "" {
		t.Fatalf("expected empty string, got %s", root)
	}
}
