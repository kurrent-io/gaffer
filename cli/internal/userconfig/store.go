// Package userconfig manages gaffer's user-level config file (one per OS
// user). The file lives at $UserConfigDir/gaffer/config.toml and is
// *co-owned* across gaffer features: the telemetry layer owns
// `[telemetry]`, future features may add `[fixtures]`, `[scaffold]`, etc.
// Store reads/writes one section at a time and round-trips unknown
// sections verbatim so concurrent owners can't trample each other.
//
// # Comment preservation (or lack of it)
//
// Store rewrites the file in BurntSushi/toml's canonical form on every
// Save. Hand-written comments and key-order from user edits do NOT
// survive a Save. config.toml is machine-written in practice (the CLI
// writes it via `gaffer config telemetry on/off`), so this is an
// acceptable trade-off; switching to an AST-preserving TOML library
// (pelletier/go-toml/v2) is the right move only when a feature lands a
// section users are expected to hand-edit with comments.
//
// # First-write race
//
// Two gaffer processes minting state simultaneously on a fresh install
// both LoadStore an empty file and both try Save. The first Save uses
// O_CREATE|O_EXCL on the final path; the loser sees ErrRaceLost and is
// expected to Reload + adopt the winner's content.
//
// O_EXCL is reliably atomic on local filesystems (ext4, apfs, ntfs) and
// on NFSv4. On NFSv2/v3 with some server implementations it's advisory
// only - the worst case there is two processes coexist briefly with
// different identities until the next mutating call. config.toml lives
// under XDG_CONFIG_HOME / Library / AppData which is local disk in
// 99.9% of installs, so the edge is documented but not engineered
// around.
package userconfig

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
)

// configFileName is the on-disk name of the user-level config file. The
// directory is $UserConfigDir/gaffer/; the file is config.toml -
// deliberately distinct from project-level gaffer.toml so the two can't
// be confused.
const configFileName = "config.toml"

// ErrRaceLost is returned by Save when the file did not exist at Load
// time but a concurrent process created it before our Save reached disk.
// Callers (the first-mint flow) handle this by Reload and adopting the
// winner's content rather than overwriting it.
var ErrRaceLost = errors.New("userconfig: another process wrote the file first")

// Store is the in-memory view of $UserConfigDir/gaffer/config.toml.
// Sections are accessed as map[string]any; unknown sections round-trip
// verbatim across Save / Reload.
type Store struct {
	dir  string
	path string
	// raw holds the entire parsed file. Empty map when the file doesn't
	// exist yet (Save creates it on first write).
	raw map[string]any
	// existedAtLoad records whether the file was present at Load. Save
	// uses this to pick between O_EXCL-on-create (first write) and
	// temp+rename (update existing).
	existedAtLoad bool
}

// EnvConfigDirOverride is the env var name that lets users (and tests / CI)
// override the resolved config directory. When set non-empty, DefaultDir
// returns its value unchanged - no /gaffer suffix is appended, so callers
// can point at any directory they own. Mirrors the convention from gh-cli
// (GH_CONFIG_DIR) and docker (DOCKER_CONFIG).
const EnvConfigDirOverride = "GAFFER_CONFIG_DIR"

// DefaultDir returns the gaffer config directory:
//   - $GAFFER_CONFIG_DIR if set (used verbatim, no /gaffer appended)
//   - else $UserConfigDir/gaffer, which is:
//     - Linux:   $XDG_CONFIG_HOME/gaffer (default $HOME/.config/gaffer)
//     - macOS:   $HOME/Library/Application Support/gaffer
//     - Windows: %AppData%/gaffer
func DefaultDir() (string, error) {
	if v := os.Getenv(EnvConfigDirOverride); v != "" {
		return v, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config dir: %w", err)
	}
	return filepath.Join(base, "gaffer"), nil
}

// Load opens the user config file in dir. A missing file (or a missing
// dir) is not an error - the returned Store is empty and ready to be
// Saved.
//
// A malformed file IS an error. The first feature to need it can
// migrate; silently dropping a user's preferences would be hostile.
func Load(dir string) (*Store, error) {
	path := filepath.Join(dir, configFileName)
	s := &Store{dir: dir, path: path, raw: map[string]any{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := toml.Unmarshal(data, &s.raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if s.raw == nil {
		s.raw = map[string]any{}
	}
	s.existedAtLoad = true
	return s, nil
}

// Reload re-reads the file from disk, discarding any in-memory changes.
// Used in the first-write race recovery path: when Save returned
// ErrRaceLost, Reload picks up the winner's content.
func (s *Store) Reload() error {
	reloaded, err := Load(s.dir)
	if err != nil {
		return err
	}
	s.raw = reloaded.raw
	s.existedAtLoad = reloaded.existedAtLoad
	return nil
}

// Path returns the absolute path the Store is bound to.
func (s *Store) Path() string { return s.path }

// Dir returns the directory the Store lives in.
func (s *Store) Dir() string { return s.dir }

// Section returns the named section as a map. Returns nil (not an empty
// map) when the section is absent or when the key exists but isn't a
// table (e.g. `telemetry = "off"` at the top level).
//
// The returned map is a *live reference* into the Store's internal
// state; mutating it bypasses SetSection's bookkeeping and is not
// supported. Treat as read-only; copy first if you need to retain it
// across a SetSection call.
//
// SectionPresent distinguishes "absent" from "wrong shape"; use it when
// callers need to surface a "you wrote a scalar where a table was
// expected" error.
func (s *Store) Section(name string) map[string]any {
	v, ok := s.raw[name]
	if !ok {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return m
}

// SectionPresent reports whether name is set in the file. Returns
// (present, isTable). Use to distinguish "missing" from "present but
// not a table" so the typed-view layer can surface a useful error
// instead of silently treating a scalar as absent.
func (s *Store) SectionPresent(name string) (present, isTable bool) {
	v, ok := s.raw[name]
	if !ok {
		return false, false
	}
	_, isMap := v.(map[string]any)
	return true, isMap
}

// SetSection replaces the named section. Passing nil or an empty map
// removes the section entirely on next Save.
func (s *Store) SetSection(name string, content map[string]any) {
	if len(content) == 0 {
		delete(s.raw, name)
		return
	}
	s.raw[name] = content
}

// Save persists the config.
//
// First write (file did not exist at Load): O_CREATE|O_EXCL directly on
// the final path. If a concurrent process beat us, returns ErrRaceLost.
// Callers handle the race via Reload + re-derive their state from the
// winner's content.
//
// Update write (file existed at Load, or first write already succeeded
// in this Store): atomic temp+rename. POSIX rename is atomic; Windows'
// MoveFileEx with MOVEFILE_REPLACE_EXISTING is the closest equivalent
// and Go's os.Rename uses it.
//
// Both paths fsync before close - a power loss between Save returning
// and the kernel flushing would otherwise leave a truncated file, and
// the next Load would either fail to parse or skip re-mint (because
// existedAtLoad would be true on the partial file).
func (s *Store) Save() error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(s.raw); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	if !s.existedAtLoad {
		// First mint: claim the file with O_EXCL so concurrent
		// first-writers can detect the race.
		f, err := os.OpenFile(s.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			return ErrRaceLost
		}
		if err != nil {
			return fmt.Errorf("create %s: %w", s.path, err)
		}
		if _, writeErr := f.Write(buf.Bytes()); writeErr != nil {
			_ = f.Close()
			_ = os.Remove(s.path)
			return fmt.Errorf("write %s: %w", s.path, writeErr)
		}
		if syncErr := f.Sync(); syncErr != nil {
			_ = f.Close()
			return fmt.Errorf("fsync %s: %w", s.path, syncErr)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("close %s: %w", s.path, err)
		}
		if err := fsyncDir(s.dir); err != nil {
			return fmt.Errorf("fsync dir %s: %w", s.dir, err)
		}
		s.existedAtLoad = true // subsequent Saves use the rename path
		return nil
	}

	// File existed at Load (or we just created it): atomic temp+rename.
	tmp, err := os.CreateTemp(s.dir, configFileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("open temp: %w", err)
	}
	tmpPath := tmp.Name()
	// Unconditional cleanup. After a successful rename, Remove returns
	// ENOENT and we ignore it; on any failure path we get the temp gone.
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("rename to %s: %w", s.path, err)
	}
	if err := fsyncDir(s.dir); err != nil {
		return fmt.Errorf("fsync dir %s: %w", s.dir, err)
	}
	return nil
}

// fsyncDir flushes the parent directory entry to disk. Without this,
// the rename (or O_EXCL create) is in-memory and a power loss can
// leave the file invisible after reboot. Best-effort on Windows: dir
// fsync isn't meaningful there (NTFS journals directory metadata
// separately), so we ignore the error.
func fsyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		// Windows returns "Access denied" / ERROR_INVALID_HANDLE for
		// dir Sync; the metadata-journaling FS makes the call moot
		// anyway. Treat as success on that platform.
		if runtime.GOOS == "windows" {
			return nil
		}
		return err
	}
	return nil
}
