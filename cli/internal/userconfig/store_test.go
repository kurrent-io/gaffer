package userconfig

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLoad_MissingFileIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := s.Section("anything"); got != nil {
		t.Errorf("Section on empty store = %v, want nil", got)
	}
}

func TestLoad_MalformedFileErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, configFileName), []byte("not a valid = toml = file"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := Load(dir)
	if err == nil {
		t.Error("Load on malformed file returned nil error")
	}
}

func TestStore_RoundTripSection(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.SetSection("telemetry", map[string]any{
		"enabled": true,
		"id":      "abc",
		"salt":    "def",
	})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	s2, err := Load(dir)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	got := s2.Section("telemetry")
	if got["enabled"] != true || got["id"] != "abc" || got["salt"] != "def" {
		t.Errorf("section = %v, want enabled=true id=abc salt=def", got)
	}
}

func TestStore_PreservesUnknownSections(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, configFileName)
	const seed = `[some_future_feature]
foo = "bar"
nested = { a = 1, b = 2 }

[other]
list = ["x", "y"]
`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.SetSection("telemetry", map[string]any{"id": "abc", "salt": "def"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	s2, err := Load(dir)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if ff := s2.Section("some_future_feature"); ff["foo"] != "bar" {
		t.Errorf("some_future_feature lost: %v", ff)
	}
	if other := s2.Section("other"); len(other) == 0 {
		t.Errorf("other section lost: %v", other)
	}
	if tel := s2.Section("telemetry"); tel["id"] != "abc" {
		t.Errorf("telemetry section round-trip: %v", tel)
	}
}

// TestStore_DocumentsCommentLoss locks in the documented behaviour:
// BurntSushi/toml's encoder canonicalises on write, so hand-written
// comments do NOT survive a Save. This test exists to alert future
// readers, NOT to assert a desired property - if a feature lands that
// adds a hand-editable section, the assertion here flips and the
// encoder swaps to an AST-preserving library (pelletier/go-toml/v2).
func TestStore_DocumentsCommentLoss(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, configFileName)
	const seed = `# this is a header comment

[telemetry]
# this comment explains enabled
enabled = true
`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.SetSection("telemetry", map[string]any{"enabled": false})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if strings.Contains(string(got), "this is a header comment") {
		t.Error("header comment unexpectedly preserved - update godoc if a fix has landed")
	}
	if strings.Contains(string(got), "this comment explains enabled") {
		t.Error("inline comment unexpectedly preserved - update godoc if a fix has landed")
	}
}

func TestStore_SetEmptySectionRemovesIt(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.SetSection("telemetry", map[string]any{"id": "x"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	s2, err := Load(dir)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	s2.SetSection("telemetry", nil)
	if err := s2.Save(); err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	s3, err := Load(dir)
	if err != nil {
		t.Fatalf("Reload 2: %v", err)
	}
	if got := s3.Section("telemetry"); got != nil {
		t.Errorf("telemetry section persisted after SetSection(nil): %v", got)
	}
}

func TestStore_SaveCreatesDirectoryAndFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "deeper")
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.SetSection("telemetry", map[string]any{"id": "x"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(s.Path()); err != nil {
		t.Errorf("file not created at %s: %v", s.Path(), err)
	}
}

func TestStore_SaveFailsWhenMkdirFails(t *testing.T) {
	// Load when the target dir doesn't exist yet (returns empty store).
	// Then plant a file at the dir path so Save's MkdirAll fails and
	// the error surfaces rather than half-writing.
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "blocker")
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := os.WriteFile(dir, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("plant blocker: %v", err)
	}
	s.SetSection("telemetry", map[string]any{"id": "x"})
	if err := s.Save(); err == nil {
		t.Error("Save returned nil when target dir is a file")
	}
}

func TestStore_AtomicWriteLeavesNoTempStraggler(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.SetSection("telemetry", map[string]any{"id": "x"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	// Update path triggers temp+rename; verify cleanup.
	s2, err := Load(dir)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	s2.SetSection("telemetry", map[string]any{"id": "y"})
	if err := s2.Save(); err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestStore_SaveTwiceOnSameInstanceWorks(t *testing.T) {
	// After the first Save flips existedAtLoad, subsequent Saves take
	// the rename path and must not retrigger ErrRaceLost.
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.SetSection("telemetry", map[string]any{"id": "first"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	s.SetSection("telemetry", map[string]any{"id": "second"})
	if err := s.Save(); err != nil {
		t.Errorf("Save 2 on same instance: %v (must not return ErrRaceLost)", err)
	}
	s2, err := Load(dir)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if got := s2.Section("telemetry"); got["id"] != "second" {
		t.Errorf("final id = %v, want \"second\"", got["id"])
	}
}

// TestStore_FirstWriteRaceReturnsSentinel: two Stores both find the
// file absent at Load, both try Save with fresh content. O_EXCL on
// first create ensures only one wins; the loser sees ErrRaceLost and
// is expected to Reload.
func TestStore_FirstWriteRaceReturnsSentinel(t *testing.T) {
	dir := t.TempDir()

	a, err := Load(dir)
	if err != nil {
		t.Fatalf("Load A: %v", err)
	}
	b, err := Load(dir)
	if err != nil {
		t.Fatalf("Load B: %v", err)
	}
	a.SetSection("telemetry", map[string]any{"id": "a"})
	b.SetSection("telemetry", map[string]any{"id": "b"})

	if err := a.Save(); err != nil {
		t.Fatalf("Save A: %v", err)
	}
	err = b.Save()
	if !errors.Is(err, ErrRaceLost) {
		t.Fatalf("Save B err = %v, want ErrRaceLost", err)
	}

	if err := b.Reload(); err != nil {
		t.Fatalf("Reload B: %v", err)
	}
	if got := b.Section("telemetry"); got["id"] != "a" {
		t.Errorf("post-Reload B sees %v; want winner a", got)
	}
}

func TestStore_ConcurrentFirstWrites(t *testing.T) {
	// 8 Stores, all Loaded before any Save starts (so all see
	// existedAtLoad=false), then released through a barrier to race
	// their Saves. Exactly one wins; seven see ErrRaceLost.
	const n = 8
	dir := t.TempDir()
	type result struct {
		label string
		err   error
	}
	results := make(chan result, n)
	stores := make([]*Store, n)
	for i := range n {
		s, err := Load(dir)
		if err != nil {
			t.Fatalf("Load %d: %v", i, err)
		}
		stores[i] = s
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {

		label := string('a' + rune(i))
		wg.Go(func() {
			stores[i].SetSection("telemetry", map[string]any{"id": label})
			<-start
			results <- result{label, stores[i].Save()}
		})
	}
	close(start)
	wg.Wait()
	close(results)

	var winners, losers int
	for r := range results {
		switch {
		case r.err == nil:
			winners++
		case errors.Is(r.err, ErrRaceLost):
			losers++
		default:
			t.Errorf("%s: unexpected Save err: %v", r.label, r.err)
		}
	}
	if winners != 1 || losers != n-1 {
		t.Errorf("winners=%d losers=%d, want 1 winner and %d losers", winners, losers, n-1)
	}

	// File must be parseable and contain one of the writers' content.
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Final Load: %v", err)
	}
	tel := s.Section("telemetry")
	id, _ := tel["id"].(string)
	if len(id) != 1 || id[0] < 'a' || id[0] >= 'a'+n {
		t.Errorf("final id = %q, want one of a..%c", id, 'a'+n-1)
	}
}

func TestStore_ReloadDiscardsInMemoryChanges(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.SetSection("telemetry", map[string]any{"id": "saved"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Stage a change in memory without saving.
	s.SetSection("telemetry", map[string]any{"id": "staged-but-not-saved"})
	if err := s.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if got := s.Section("telemetry"); got["id"] != "saved" {
		t.Errorf("Reload kept in-memory change: %v", got)
	}
}

func TestDefaultDir_EndsInGaffer(t *testing.T) {
	t.Setenv(EnvConfigDirOverride, "")
	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if !strings.HasSuffix(dir, string(os.PathSeparator)+"gaffer") {
		t.Errorf("DefaultDir = %s, want trailing /gaffer", dir)
	}
}

func TestDefaultDir_EnvOverrideUsedVerbatim(t *testing.T) {
	override := t.TempDir()
	t.Setenv(EnvConfigDirOverride, override)
	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if dir != override {
		t.Errorf("DefaultDir = %s, want %s (env override used verbatim, no /gaffer suffix)", dir, override)
	}
}

func TestSectionPresent_DistinguishesAbsentFromScalar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, configFileName)
	const seed = `scalar_at_top = "off"

[real_table]
k = "v"
`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// scalar_at_top: present, not a table
	if present, isTable := s.SectionPresent("scalar_at_top"); !present || isTable {
		t.Errorf("scalar_at_top: present=%v isTable=%v, want present=true isTable=false", present, isTable)
	}
	// real_table: present, is a table
	if present, isTable := s.SectionPresent("real_table"); !present || !isTable {
		t.Errorf("real_table: present=%v isTable=%v, want both true", present, isTable)
	}
	// truly_absent: not present
	if present, _ := s.SectionPresent("truly_absent"); present {
		t.Errorf("truly_absent: present=true, want false")
	}
}

func TestStore_SaveAfterReload(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	s.SetSection("telemetry", map[string]any{"id": "first"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	if err := s.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	s.SetSection("telemetry", map[string]any{"id": "second"})
	if err := s.Save(); err != nil {
		t.Errorf("Save after Reload: %v", err)
	}
}

func TestStore_SaveOnEmptyRawWritesEmptyFile(t *testing.T) {
	// A fresh Store with no sections set, Save'd, produces an empty
	// (zero-byte or whitespace-only) file. Document the behaviour.
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(s.Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() > 32 {
		t.Errorf("empty Save produced %d bytes, want near-zero", info.Size())
	}
	// And the file Loads cleanly with no sections.
	s2, err := Load(dir)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if got := s2.Section("anything"); got != nil {
		t.Errorf("Section on empty file = %v, want nil", got)
	}
}
