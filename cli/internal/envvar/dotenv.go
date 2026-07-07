package envvar

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/joho/godotenv"
)

// Load reads the base .env file from projectDir into the process
// environment so every env-var read - telemetry opt-out, update-check,
// credentials, connection ${VAR} expansion - honours it. Existing
// process environment variables are never overwritten: a real shell
// variable wins over .env, matching the npm dotenv default and letting
// `VAR=x gaffer ...` (and CI secret injection) override the file.
//
// Called once early at process startup so reads that happen before any
// DB connection still see .env. A no-op when projectDir is empty or no
// .env exists; an unreadable or malformed .env returns an error.
func Load(projectDir string) error {
	if projectDir == "" {
		return nil
	}
	// godotenv surfaces a missing file as fs.ErrNotExist, which is the
	// "no .env" no-op. Any other error - unreadable file, malformed
	// contents - is a real problem and is returned, not swallowed. The
	// godotenv error is deliberately NOT wrapped: its parse errors echo
	// raw file bytes, which for a credential-bearing .env would leak
	// secrets into the message.
	if err := godotenv.Load(filepath.Join(projectDir, ".env")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return errors.New("malformed .env")
	}
	loadedRoots.Store(canonicalRoot(projectDir), true)
	return nil
}

// loadedRoots records the project roots whose base .env has been loaded into
// the process env (including the no-file no-op), so Loaded can vouch for a
// root. sync.Map: written from command startup / Connect, read from
// background goroutines (the drift check's resolution).
var loadedRoots sync.Map

// Loaded reports whether Load ran for this project root. Credentials read
// via the process-env fallback are only complete once the base .env is
// loaded; target.Resolve refuses to resolve a root with an unloaded .env
// rather than silently produce empty credentials (the UI-1820 shape).
func Loaded(projectDir string) bool {
	if projectDir == "" {
		return true
	}
	_, ok := loadedRoots.Load(canonicalRoot(projectDir))
	return ok
}

// canonicalRoot normalizes a project root so Load and Loaded agree on the
// key regardless of how the caller spelled the path.
func canonicalRoot(dir string) string {
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return filepath.Clean(dir)
}

// Credentials returns the KurrentDB username and password resolved from
// the given overlay with the same precedence as Expand: shell env >
// overlay (.env.<env>) > base .env. So per-environment credentials can
// live in .env.<env>. Pass the overlay from Overlay (nil for an ad-hoc
// connection with no environment); resolving it once in the caller lets
// Expand and Credentials share a single read of the file.
func Credentials(overlay map[string]string) (username, password string) {
	username, _ = resolveVar("KURRENTDB_USERNAME", overlay)
	password, _ = resolveVar("KURRENTDB_PASSWORD", overlay)
	return username, password
}

// OAuthClientSecret returns the OAuth client secret resolved from the overlay
// with the same precedence as Credentials. Its presence selects the
// non-interactive client-credentials grant; an empty result means an
// interactive login (the token stored by `gaffer auth`) is used instead.
func OAuthClientSecret(overlay map[string]string) string {
	secret, _ := resolveVar("KURRENTDB_OAUTH_CLIENT_SECRET", overlay)
	return secret
}

// shellEnv is the process environment captured by Snapshot before Load
// layered any .env on top. It lets Expand apply shell > .env.<env> >
// .env precedence: after Load, the process env no longer distinguishes
// a real shell variable from a base-.env one, so the per-env overlay
// can't tell which to override. The snapshot draws that line.
var shellEnv map[string]string

// Snapshot records the current process environment as the "shell"
// layer. Call once at process startup, before Load. A nil snapshot
// (Snapshot never called) leaves the shell layer empty, so Expand falls
// back to the live process environment.
func Snapshot() {
	env := os.Environ()
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if before, after, ok := strings.Cut(kv, "="); ok {
			m[before] = after
		}
	}
	shellEnv = m
}

// braceVarPattern matches the braced ${VAR} interpolation form only. A
// bare `$` (e.g. inside an inline connection-string password) is left
// untouched.
var braceVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// Expand substitutes ${VAR} references in s, resolving each name with
// precedence shell env > overlay (.env.<env>) > base .env. The shell
// layer is the Snapshot taken before Load; the base layer is the live
// process environment (which Load populated from .env). The overlay is
// the per-environment .env.<env> map from Overlay (nil when no
// environment is selected) - a non-mutating layer between shell and base
// that the caller resolves once and shares with Credentials, so a
// long-running server can resolve different envs per call without
// leaking one into the next.
//
// Returns an error naming any referenced variable that isn't set (a
// missing secret fails loudly rather than expanding to "").
func Expand(s string, overlay map[string]string) (string, error) {
	var missing []string
	out := braceVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-1]
		if v, ok := resolveVar(name, overlay); ok {
			return v
		}
		missing = append(missing, name)
		return match
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("undefined environment variable(s): %s", strings.Join(dedupeSorted(missing), ", "))
	}
	return out, nil
}

// resolveVar applies the shell > overlay (.env.<env>) > base layering.
// With a Snapshot, a name in the shell layer wins outright; otherwise
// the per-env overlay wins over the base process env (which Load
// populated from .env).
//
// Without a Snapshot (shellEnv nil) the shell and base layers are
// indistinguishable in the process env, so we can't let the overlay win
// over what might be a real shell variable: the process env takes
// precedence and the overlay only fills names it doesn't define. This
// keeps a Connect that runs before Snapshot from silently letting
// .env.<env> override a real environment variable.
func resolveVar(name string, overlay map[string]string) (string, bool) {
	if shellEnv != nil {
		if v, ok := shellEnv[name]; ok {
			return v, true
		}
		if v, ok := overlay[name]; ok {
			return v, true
		}
		return os.LookupEnv(name)
	}
	if v, ok := os.LookupEnv(name); ok {
		return v, true
	}
	if v, ok := overlay[name]; ok {
		return v, true
	}
	return "", false
}

// Overlay reads the per-environment .env.<envName> overlay from
// projectDir, for the caller to pass to Expand and Credentials. It
// returns nil when there's no project or no environment to key on (the
// overlay only has meaning once an environment is selected). A missing
// file is (nil, nil); a malformed file is an error.
func Overlay(projectDir, envName string) (map[string]string, error) {
	if projectDir == "" || envName == "" {
		return nil, nil
	}
	return readEnvFile(filepath.Join(projectDir, ".env."+envName))
}

// readEnvFile parses a .env-format file into a map without touching the
// process environment. A missing file is (nil, nil); a malformed file
// is an error. The godotenv error is deliberately NOT wrapped: its
// parse errors echo raw file bytes, which for a credential-bearing
// .env.<env> would leak secrets into the message.
func readEnvFile(path string) (map[string]string, error) {
	m, err := godotenv.Read(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("malformed %s", filepath.Base(path))
	}
	return m, nil
}

func dedupeSorted(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}
