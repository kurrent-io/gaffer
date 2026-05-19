package updatecheck

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// cacheFileName is the on-disk name of the update-check cache.
const cacheFileName = "update-check.json"

// DefaultTTL is how long a fetched latest_version is trusted before
// the cache is treated as stale and a background refresh kicks off.
// 24h matches the ticket spec and the update-notifier convention.
const DefaultTTL = 24 * time.Hour

// Cache is the on-disk record of the last npm-registry lookup. Empty
// when the file is missing or unreadable - we treat that as "no cache,
// refetch" rather than an error.
type Cache struct {
	CheckedAt          time.Time `json:"checked_at"`
	CheckedWithVersion string    `json:"checked_with_version"`
	LatestVersion      string    `json:"latest_version"`
}

// LoadCache reads update-check.json from dir. A missing file, a
// missing dir, or a malformed file all return an empty Cache and a
// nil error - update-check is best-effort and a corrupt cache should
// not surface to the user. The empty value's IsStale always reports
// true, so the caller will refetch.
func LoadCache(dir string) (Cache, error) {
	path := filepath.Join(dir, cacheFileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Cache{}, nil
	}
	if err != nil {
		return Cache{}, nil
	}
	var c Cache
	if err := json.Unmarshal(data, &c); err != nil {
		return Cache{}, nil
	}
	return c, nil
}

// SaveCache writes c to dir/update-check.json atomically (temp file +
// rename). Creates dir if missing. Deliberately does NOT fsync the
// directory: a power-loss-lost cache file just means the next run
// refetches, which is free. Don't "fix" this to match userconfig.Save -
// the durability tradeoff is intentional.
func SaveCache(dir string, c Cache) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}
	tmp, err := os.CreateTemp(dir, cacheFileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("create tempfile: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write tempfile: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close tempfile: %w", err)
	}
	if err := os.Rename(tmpPath, filepath.Join(dir, cacheFileName)); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename tempfile: %w", err)
	}
	return nil
}

// IsStale reports whether c should be refetched. True when:
//   - The cache is empty (zero CheckedAt or empty LatestVersion).
//   - More than ttl has elapsed since CheckedAt.
//   - CheckedWithVersion differs from current. After an upgrade the
//     old cache's notion of "latest" is irrelevant; refetch immediately
//     rather than waiting up to ttl.
func (c Cache) IsStale(now time.Time, current string, ttl time.Duration) bool {
	if c.CheckedAt.IsZero() || c.LatestVersion == "" {
		return true
	}
	if c.CheckedWithVersion != current {
		return true
	}
	return now.Sub(c.CheckedAt) > ttl
}
