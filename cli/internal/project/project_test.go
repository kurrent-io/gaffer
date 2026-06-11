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

func TestFindRootFromBounded_StopsAtBound(t *testing.T) {
	// gaffer.toml in an ancestor above the bound must not be found - the
	// bounded walk stops at the bound, though an unbounded walk would
	// cross it and pick up the stray config.
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "gaffer.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "home")
	start := filepath.Join(home, "work")
	if err := os.MkdirAll(start, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := FindRootFromBounded(start, home); got != "" {
		t.Fatalf("bounded walk should stop at %s, got %s", home, got)
	}
	if got := FindRootFrom(start); got != base {
		t.Fatalf("unbounded walk should find %s, got %s", base, got)
	}
}

func TestFindRootFromBounded_FindsBelowBound(t *testing.T) {
	// A project under the bound is still found.
	home := t.TempDir()
	proj := filepath.Join(home, "proj")
	start := filepath.Join(proj, "sub")
	if err := os.MkdirAll(start, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "gaffer.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := FindRootFromBounded(start, home); got != proj {
		t.Fatalf("expected %s, got %s", proj, got)
	}
}
