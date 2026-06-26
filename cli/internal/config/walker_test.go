package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// touch creates an empty file at path, mkdir-p'ing parents. Test helper.
func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWalkConfigs_EmptyDirectory(t *testing.T) {
	got, err := WalkConfigs(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no results, got %v", got)
	}
}

func TestWalkConfigs_FindsRootLevelConfig(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "gaffer.toml"))
	got, err := WalkConfigs(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "gaffer.toml")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWalkConfigs_FindsNestedConfigs(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "a", "gaffer.toml"))
	touch(t, filepath.Join(dir, "b", "c", "gaffer.toml"))
	got, err := WalkConfigs(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(dir, "a", "gaffer.toml"),
		filepath.Join(dir, "b", "c", "gaffer.toml"),
	}
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWalkConfigs_SkipsHardcodedNoiseDirs(t *testing.T) {
	dir := t.TempDir()
	// Buried inside each of the hardcoded skip dirs - none should be found.
	touch(t, filepath.Join(dir, ".git", "gaffer.toml"))
	touch(t, filepath.Join(dir, "node_modules", "x", "gaffer.toml"))
	touch(t, filepath.Join(dir, "vendor", "gaffer.toml"))
	// Visible at root.
	touch(t, filepath.Join(dir, "gaffer.toml"))

	got, err := WalkConfigs(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "gaffer.toml")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWalkConfigs_RootNamedNodeModulesIsNotSkipped(t *testing.T) {
	// Edge case: the root happens to be named `node_modules`. We must
	// not skip the root - the user is pointing the walker at it
	// deliberately.
	parent := t.TempDir()
	dir := filepath.Join(parent, "node_modules")
	touch(t, filepath.Join(dir, "gaffer.toml"))

	got, err := WalkConfigs(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != filepath.Join(dir, "gaffer.toml") {
		t.Fatalf("expected root config to be found, got %v", got)
	}
}

func TestWalkConfigs_HonorsRootGitignore(t *testing.T) {
	// Verifies the outcome: a gaffer.toml inside a gitignored
	// directory doesn't appear in the result set. Doesn't
	// distinguish whether the dir was skipped (cheap) or descended-
	// then-file-filtered (more work but same outcome) - both
	// satisfy the contract. The walker uses MatchesPath(rel) ||
	// MatchesPath(rel+"/") on directories so dir-only patterns
	// like `build/` actually prune descent rather than relying on
	// per-file filtering, but that's a perf concern and isn't
	// directly observable from a result-equality test.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".gitignore"), "build/\n")
	touch(t, filepath.Join(dir, "build", "gaffer.toml"))
	touch(t, filepath.Join(dir, "src", "gaffer.toml")) // visible
	touch(t, filepath.Join(dir, "gaffer.toml"))        // visible

	got, err := WalkConfigs(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(dir, "gaffer.toml"),
		filepath.Join(dir, "src", "gaffer.toml"),
	}
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWalkConfigs_DoesNotConsultNestedGitignores(t *testing.T) {
	// Per the LSP plan: only the root .gitignore is honored. Nested
	// .gitignore files are intentionally ignored - matching
	// pulumi/wails/bearer's behavior. Pin so a future contributor
	// doesn't quietly add hierarchy support.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "sub", ".gitignore"), "gaffer.toml\n")
	touch(t, filepath.Join(dir, "sub", "gaffer.toml"))

	got, err := WalkConfigs(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "sub", "gaffer.toml")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWalkConfigs_GitignoreRespectsFilePatterns(t *testing.T) {
	// `gaffer.toml` literally listed in .gitignore at the root - we
	// honor it. Pathological but the rule is the rule.
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".gitignore"), "gaffer.toml\n")
	touch(t, filepath.Join(dir, "gaffer.toml"))
	touch(t, filepath.Join(dir, "sub", "gaffer.toml"))

	got, err := WalkConfigs(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no results when gaffer.toml is gitignored, got %v", got)
	}
}

func TestWalkConfigs_SymlinkedRootIsWalked(t *testing.T) {
	// If the user points the LSP at a workspace root that's itself
	// a symlink (uncommon but legitimate - e.g. ~/projects/foo
	// symlinked to /mnt/storage/foo), the walker resolves it
	// upfront via filepath.EvalSymlinks so the descent works.
	// Results are reported under the resolved path. Nested symlinks
	// below the root remain unfollowed.
	parent := t.TempDir()
	real := filepath.Join(parent, "real")
	link := filepath.Join(parent, "link")
	touch(t, filepath.Join(real, "gaffer.toml"))
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}

	got, err := WalkConfigs(context.Background(), link)
	if err != nil {
		t.Fatal(err)
	}
	// Expect the resolved real path; t.TempDir() may itself
	// resolve via macOS /private/var symlinks etc., so use
	// EvalSymlinks on the expected value too rather than naively
	// equality-checking.
	wantReal, err := filepath.EvalSymlinks(filepath.Join(real, "gaffer.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != wantReal {
		t.Fatalf("expected [%s], got %v", wantReal, got)
	}
}

func TestWalkConfigs_DoesNotFollowSymlinkedDirectories(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	touch(t, filepath.Join(target, "gaffer.toml"))
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}

	got, err := WalkConfigs(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	// Only one find: the real path through `target`. The symlink at
	// `link` is not descended.
	want := []string{filepath.Join(target, "gaffer.toml")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWalkConfigs_DoesNotFollowSymlinkedFiles(t *testing.T) {
	// A symlink at `<root>/gaffer.toml` pointing at a real file
	// outside the workspace must NOT be reported as a discovered
	// config - else a hostile or carelessly-set-up workspace could
	// pull arbitrary files into the LSP's view. The target is
	// intentionally also named `gaffer.toml` so a regression
	// (following the link, then matching by basename) would fire
	// the assertion.
	parent := t.TempDir()
	dir := filepath.Join(parent, "ws")
	outside := filepath.Join(parent, "outside")
	touch(t, filepath.Join(outside, "gaffer.toml"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "gaffer.toml")
	if err := os.Symlink(filepath.Join(outside, "gaffer.toml"), link); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}

	got, err := WalkConfigs(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no results - symlinked file must not be followed, got %v", got)
	}
}

func TestWalkConfigs_DoesNotMatchSimilarFilenames(t *testing.T) {
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "gaffer.toml.bak"))
	touch(t, filepath.Join(dir, "not-gaffer.toml"))
	touch(t, filepath.Join(dir, "gaffer.json"))

	got, err := WalkConfigs(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected nothing, got %v", got)
	}
}

func TestWalkConfigs_DeterministicOrder(t *testing.T) {
	// Two configs - sorted lexicographic order regardless of FS
	// enumeration order. Pin so a future caller can rely on
	// deterministic output (e.g. for stable diagnostics ordering).
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "z", "gaffer.toml"))
	touch(t, filepath.Join(dir, "a", "gaffer.toml"))

	got, err := WalkConfigs(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(dir, "a", "gaffer.toml"),
		filepath.Join(dir, "z", "gaffer.toml"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWalkConfigs_RespectsContextCancellation(t *testing.T) {
	dir := t.TempDir()
	// Some content so the walker has work to do.
	for range 50 {
		touch(t, filepath.Join(dir, "sub", "x", "y", "file.txt"))
	}
	touch(t, filepath.Join(dir, "gaffer.toml"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled

	_, err := WalkConfigs(ctx, dir)
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestWalkConfigs_NonexistentRootReturnsError(t *testing.T) {
	_, err := WalkConfigs(context.Background(), filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("expected error for nonexistent root")
	}
}

func TestWalkConfigs_ReturnsAbsolutePaths(t *testing.T) {
	// Even if caller passes a relative root, results are absolute.
	// LSP wire wants absolute paths; pin the contract.
	dir := t.TempDir()
	touch(t, filepath.Join(dir, "gaffer.toml"))

	t.Chdir(dir)

	got, err := WalkConfigs(context.Background(), ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !filepath.IsAbs(got[0]) {
		t.Fatalf("expected one absolute path, got %v", got)
	}
}
