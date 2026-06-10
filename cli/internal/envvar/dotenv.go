package envvar

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

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
	return nil
}

// Credentials returns the KurrentDB username and password for the
// selected environment, resolved with the same precedence as Expand:
// shell env > .env.<envName> > base .env. So per-environment credentials
// can live in .env.<env>. envName is "" for an ad-hoc --connection with
// no environment, in which case only shell + base .env apply.
//
// The .env.<envName> read is best-effort: Connect resolves the
// connection via Expand first, which already surfaces a malformed
// overlay, so a read failure here just falls back to shell + base .env.
func Credentials(projectDir, envName string) (username, password string) {
	overlay, _ := overlayFor(projectDir, envName)
	username, _ = resolveVar("KURRENTDB_USERNAME", overlay)
	password, _ = resolveVar("KURRENTDB_PASSWORD", overlay)
	return username, password
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
		if i := strings.IndexByte(kv, '='); i >= 0 {
			m[kv[:i]] = kv[i+1:]
		}
	}
	shellEnv = m
}

// braceVarPattern matches the braced ${VAR} interpolation form only. A
// bare `$` (e.g. inside an inline connection-string password) is left
// untouched.
var braceVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// Expand substitutes ${VAR} references in s, resolving each name with
// precedence shell env > .env.<envName> > base .env. The shell layer is
// the Snapshot taken before Load; the base layer is the live process
// environment (which Load populated from .env). When envName and
// projectDir are set, .env.<envName> is read from projectDir as a
// non-mutating overlay between them - so a long-running server can
// resolve different envs per call without leaking one into the next.
//
// Returns an error naming any referenced variable that isn't set (a
// missing secret fails loudly rather than expanding to ""), or if
// .env.<envName> exists but is malformed.
func Expand(s, projectDir, envName string) (string, error) {
	overlay, err := overlayFor(projectDir, envName)
	if err != nil {
		return "", err
	}

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
// A name present in the shell snapshot wins outright; otherwise the
// per-env overlay wins over the base process env. os.LookupEnv covers
// both the shell (which also sits in the process env) and the base
// .env, but the explicit shell check is what lets the overlay override
// a base-.env value without overriding a real shell value.
func resolveVar(name string, overlay map[string]string) (string, bool) {
	if v, ok := shellEnv[name]; ok {
		return v, true
	}
	if v, ok := overlay[name]; ok {
		return v, true
	}
	return os.LookupEnv(name)
}

// overlayFor reads the per-environment .env.<envName> overlay from
// projectDir. It returns nil when there's no project or no environment
// to key on (the overlay only has meaning once an environment is
// selected). A missing file is (nil, nil); a malformed file is an error.
func overlayFor(projectDir, envName string) (map[string]string, error) {
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
	sort.Strings(out)
	return out
}
