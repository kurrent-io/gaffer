package pathutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasWindowsDrivePrefix(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"", false},
		{"C", false},
		{"foo.js", false},
		{"projections/foo.js", false},
		{"C:", true},
		{"C:\\foo.js", true},
		{"C:/foo.js", true},
		{"C:foo.js", true}, // drive-relative
		{"c:\\foo.js", true},
		{"z:\\foo.js", true},
		{"1:\\foo.js", false}, // digit, not a drive letter
		{":\\foo.js", false},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			if got := HasWindowsDrivePrefix(tc.input); got != tc.want {
				t.Errorf("HasWindowsDrivePrefix(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestEscapesRoot(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"foo.js", false},
		{"projections/foo.js", false},
		{"..hidden.js", false}, // literal name, not traversal
		{"foo/..bar.js", false},
		{"..", true},
		{"../foo.js", true},
		{"foo/../../bar.js", true},
		{"..\\foo.js", true}, // backslash form, normalised
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			if got := EscapesRoot(tc.input); got != tc.want {
				t.Errorf("EscapesRoot(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestResolveAncestorSymlinks_LeafExists(t *testing.T) {
	dir := t.TempDir()
	resolved, err := ResolveAncestorSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != dir {
		// On macOS, t.TempDir() may itself be under a symlink
		// (e.g. /var -> /private/var). Just verify it points at the
		// same inode rather than asserting string equality.
		if !sameDir(t, resolved, dir) {
			t.Errorf("got %q, want same as %q", resolved, dir)
		}
	}
}

func TestResolveAncestorSymlinks_LeafMissing(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does", "not", "exist", "file.js")
	resolved, err := ResolveAncestorSymlinks(missing)
	if err != nil {
		t.Fatal(err)
	}
	// Resolution walks up to `dir` (which exists), then rejoins.
	want := filepath.Join(dir, "does", "not", "exist", "file.js")
	if !sameDir(t, filepath.Dir(resolved), filepath.Dir(want)) || filepath.Base(resolved) != filepath.Base(want) {
		t.Errorf("got %q, want suffix %q under resolved %q", resolved, want, dir)
	}
}

func TestResolveAncestorSymlinks_FollowsAncestorLink(t *testing.T) {
	// Symlink several levels up from the not-yet-existing leaf.
	// resolveAncestorSymlinks walks past the missing dirs to find
	// the link.
	realDir := t.TempDir()
	parent := t.TempDir()
	link := filepath.Join(parent, "via-link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	missing := filepath.Join(link, "deep", "nested", "leaf.js")

	resolved, err := ResolveAncestorSymlinks(missing)
	if err != nil {
		t.Fatal(err)
	}
	// Resolved path should anchor on realDir, not via-link.
	want := filepath.Join(realDir, "deep", "nested", "leaf.js")
	if !sameDir(t, filepath.Dir(filepath.Dir(filepath.Dir(resolved))), realDir) {
		t.Errorf("expected resolution to anchor on %q, got %q", realDir, resolved)
	}
	if filepath.Base(resolved) != filepath.Base(want) {
		t.Errorf("leaf: got %q, want %q", resolved, want)
	}
}

func TestIsInsideRoot_PathInsideRoot(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, "projections", "foo.js")
	inside, err := IsInsideRoot(root, abs)
	if err != nil {
		t.Fatal(err)
	}
	if !inside {
		t.Error("expected abs path under root to be inside")
	}
}

func TestIsInsideRoot_PathOutsideRoot(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(t.TempDir(), "foo.js")
	inside, err := IsInsideRoot(root, abs)
	if err != nil {
		t.Fatal(err)
	}
	if inside {
		t.Error("expected abs path in a different tmpdir to be outside")
	}
}

func TestIsInsideRoot_SymlinkedRoot(t *testing.T) {
	// Project lives at realRoot; user reaches it via a symlink.
	// IsInsideRoot must resolve both sides so abs under realRoot
	// still reports inside.
	realRoot := t.TempDir()
	parent := t.TempDir()
	symRoot := filepath.Join(parent, "via-link")
	if err := os.Symlink(realRoot, symRoot); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	abs := filepath.Join(realRoot, "projections", "foo.js")
	inside, err := IsInsideRoot(symRoot, abs)
	if err != nil {
		t.Fatal(err)
	}
	if !inside {
		t.Error("expected symlinked-root resolution to land inside")
	}
}

func TestIsInsideRoot_SymlinkEscape(t *testing.T) {
	// An in-tree symlink pointing outside must report as not inside.
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	abs := filepath.Join(link, "stolen.js")
	inside, err := IsInsideRoot(root, abs)
	if err != nil {
		t.Fatal(err)
	}
	if inside {
		t.Error("expected escape-symlink target to be outside the resolved root")
	}
}

func TestWalkUpFor_FindsMarkerAtStart(t *testing.T) {
	dir := t.TempDir()
	marker := "gaffer.toml"
	if err := os.WriteFile(filepath.Join(dir, marker), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := WalkUpFor(dir, marker, ""); !sameDir(t, got, dir) {
		t.Errorf("got %q, want same as %q", got, dir)
	}
}

func TestWalkUpFor_WalksUpToFindMarker(t *testing.T) {
	root := t.TempDir()
	marker := "gaffer.toml"
	if err := os.WriteFile(filepath.Join(root, marker), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := WalkUpFor(nested, marker, ""); !sameDir(t, got, root) {
		t.Errorf("got %q, want %q", got, root)
	}
}

func TestWalkUpFor_ReturnsEmptyWhenMarkerNotFound(t *testing.T) {
	dir := t.TempDir()
	if got := WalkUpFor(dir, "gaffer.toml", ""); got != "" {
		t.Errorf("got %q, want \"\"", got)
	}
}

func TestWalkUpFor_StopsAtBound(t *testing.T) {
	// Marker exists at the bound; the walk must NOT see it.
	bound := t.TempDir()
	marker := "gaffer.toml"
	if err := os.WriteFile(filepath.Join(bound, marker), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(bound, "child")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := WalkUpFor(nested, marker, bound); got != "" {
		t.Errorf("expected bound to prevent finding marker, got %q", got)
	}
}

func TestWalkUpFor_EmptyStartReturnsEmpty(t *testing.T) {
	if got := WalkUpFor("", "gaffer.toml", ""); got != "" {
		t.Errorf("got %q, want \"\"", got)
	}
}

func TestAnchorUnder(t *testing.T) {
	cases := []struct {
		name, root, p, want string
	}{
		{"relative anchors under root", "/proj", "events.json", "/proj/events.json"},
		{"absolute returns as-is", "/proj", "/etc/events.json", "/etc/events.json"},
		{"cleans extra separators", "/proj", "sub//file.json", "/proj/sub/file.json"},
		{"cleans dot segments", "/proj", "sub/./file.json", "/proj/sub/file.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AnchorUnder(tc.root, tc.p); got != tc.want {
				t.Errorf("AnchorUnder(%q, %q) = %q, want %q", tc.root, tc.p, got, tc.want)
			}
		})
	}
}

// sameDir checks whether two paths refer to the same directory on
// disk, tolerating /var <-> /private/var-style symlinks the host OS
// may introduce.
func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = a
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = b
	}
	return ra == rb
}
