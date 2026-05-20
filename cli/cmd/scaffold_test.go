package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveScaffoldRelPath_CwdRelative(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "my", "great", "projections")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)

	rel, err := resolveScaffoldRelPath("counter.js", root)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("my", "great", "projections", "counter.js")
	if rel != want {
		t.Errorf("rel: got %q, want %q", rel, want)
	}
}

func TestResolveScaffoldRelPath_AbsoluteInsideRoot(t *testing.T) {
	root := t.TempDir()
	t.Chdir(t.TempDir())

	abs := filepath.Join(root, "projections", "counter.js")
	rel, err := resolveScaffoldRelPath(abs, root)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("projections", "counter.js")
	if rel != want {
		t.Errorf("rel: got %q, want %q", rel, want)
	}
}

func TestResolveScaffoldRelPath_SymlinkedRoot(t *testing.T) {
	// Project lives at one real path; user reaches it via a
	// symlink. project.FindRoot would return the symlinked form;
	// an absolute arg via the real path must still resolve to a
	// project-root-relative result, not "outside the project root".
	realRoot := t.TempDir()
	parent := t.TempDir()
	symRoot := filepath.Join(parent, "via-link")
	if err := os.Symlink(realRoot, symRoot); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}
	t.Chdir(t.TempDir())

	abs := filepath.Join(realRoot, "projections", "counter.js")
	rel, err := resolveScaffoldRelPath(abs, symRoot)
	if err != nil {
		t.Fatalf("expected symlinked-root resolution to succeed, got: %v", err)
	}
	want := filepath.Join("projections", "counter.js")
	if rel != want {
		t.Errorf("rel: got %q, want %q", rel, want)
	}
}

func TestResolveScaffoldRelPath_OutsideRoot(t *testing.T) {
	// An absolute path that lives outside the project must be
	// rejected at the cmd layer so the error message echoes the
	// user's original input (not the derived "../..." string).
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "counter.js")
	t.Chdir(root)

	_, err := resolveScaffoldRelPath(outside, root)
	if err == nil {
		t.Fatal("expected error for path outside project root")
	}
	if !strings.Contains(err.Error(), outside) {
		t.Errorf("expected error to echo original input %q, got: %v", outside, err)
	}
	if !strings.Contains(err.Error(), "outside the project root") {
		t.Errorf("expected 'outside the project root', got: %v", err)
	}
}
