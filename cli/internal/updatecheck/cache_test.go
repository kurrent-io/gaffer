package updatecheck

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadCache_MissingDirIsEmptyNoError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	c, err := LoadCache(dir)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if c != (Cache{}) {
		t.Errorf("LoadCache on missing dir = %+v, want zero value", c)
	}
}

func TestLoadCache_MissingFileIsEmptyNoError(t *testing.T) {
	c, err := LoadCache(t.TempDir())
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if c != (Cache{}) {
		t.Errorf("LoadCache on empty dir = %+v, want zero value", c)
	}
}

func TestLoadCache_MalformedFileIsEmptyNoError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, cacheFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	c, err := LoadCache(dir)
	if err != nil {
		t.Errorf("LoadCache returned error on malformed JSON: %v (want silent empty)", err)
	}
	if c != (Cache{}) {
		t.Errorf("LoadCache on malformed JSON = %+v, want zero value", c)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	want := Cache{
		CheckedAt:          time.Date(2026, 5, 19, 10, 0, 0, 0, time.UTC),
		CheckedWithVersion: "0.1.3",
		LatestVersion:      "0.2.0",
	}
	if err := SaveCache(dir, want); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}
	got, err := LoadCache(dir)
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if !got.CheckedAt.Equal(want.CheckedAt) || got.CheckedWithVersion != want.CheckedWithVersion || got.LatestVersion != want.LatestVersion {
		t.Errorf("roundtrip: got %+v, want %+v", got, want)
	}
}

func TestSaveCache_CreatesMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "gaffer")
	c := Cache{CheckedAt: time.Now(), CheckedWithVersion: "0.1.3", LatestVersion: "0.1.3"}
	if err := SaveCache(dir, c); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, cacheFileName)); err != nil {
		t.Errorf("expected cache file at %s: %v", dir, err)
	}
}

func TestSaveCache_AtomicNoStaleTempLeft(t *testing.T) {
	dir := t.TempDir()
	c := Cache{CheckedAt: time.Now(), CheckedWithVersion: "0.1.3", LatestVersion: "0.2.0"}
	if err := SaveCache(dir, c); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != cacheFileName {
			t.Errorf("unexpected leftover file in cache dir: %s", e.Name())
		}
	}
}

func TestIsStale(t *testing.T) {
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		cache   Cache
		current string
		want    bool
	}{
		{
			name:    "empty cache stale",
			cache:   Cache{},
			current: "0.1.3",
			want:    true,
		},
		{
			name:    "zero CheckedAt stale",
			cache:   Cache{LatestVersion: "0.2.0", CheckedWithVersion: "0.1.3"},
			current: "0.1.3",
			want:    true,
		},
		{
			name:    "empty LatestVersion stale",
			cache:   Cache{CheckedAt: now.Add(-time.Hour), CheckedWithVersion: "0.1.3"},
			current: "0.1.3",
			want:    true,
		},
		{
			name:    "within TTL and version matches",
			cache:   Cache{CheckedAt: now.Add(-time.Hour), CheckedWithVersion: "0.1.3", LatestVersion: "0.2.0"},
			current: "0.1.3",
			want:    false,
		},
		{
			name:    "past TTL even with matching version",
			cache:   Cache{CheckedAt: now.Add(-25 * time.Hour), CheckedWithVersion: "0.1.3", LatestVersion: "0.2.0"},
			current: "0.1.3",
			want:    true,
		},
		{
			name:    "version mismatch even within TTL",
			cache:   Cache{CheckedAt: now.Add(-time.Hour), CheckedWithVersion: "0.1.2", LatestVersion: "0.2.0"},
			current: "0.1.3",
			want:    true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cache.IsStale(now, tc.current, DefaultTTL); got != tc.want {
				t.Errorf("IsStale = %v, want %v", got, tc.want)
			}
		})
	}
}
