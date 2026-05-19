package updatecheck

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnvCacheDirOverride lets users (and tests / CI) point the update-check
// cache at a directory they own. When set non-empty, DefaultDir returns
// its value unchanged - no /gaffer suffix is appended. Mirrors the
// userconfig.EnvConfigDirOverride convention.
const EnvCacheDirOverride = "GAFFER_CACHE_DIR"

// DefaultDir returns the gaffer cache directory:
//   - $GAFFER_CACHE_DIR if set (used verbatim, no /gaffer appended)
//   - else $UserCacheDir/gaffer, which is:
//   - Linux:   $XDG_CACHE_HOME/gaffer (default $HOME/.cache/gaffer)
//   - macOS:   $HOME/Library/Caches/gaffer
//   - Windows: %LocalAppData%/gaffer
//
// Failure to resolve the base cache dir (locked-down environments with
// no $HOME / %LocalAppData%) returns an error. Callers treat that as
// "no cache, no fetch, no notice" rather than falling back to TempDir -
// a cache that vanishes on reboot is worse than no cache.
func DefaultDir() (string, error) {
	if v := os.Getenv(EnvCacheDirOverride); v != "" {
		return v, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache dir: %w", err)
	}
	return filepath.Join(base, "gaffer"), nil
}
