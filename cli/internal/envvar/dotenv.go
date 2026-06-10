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
	// contents - is a real problem and is returned, not swallowed.
	if err := godotenv.Load(filepath.Join(projectDir, ".env")); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("loading .env: %w", err)
	}
	return nil
}

// Credentials returns the KurrentDB username and password from the
// environment (populated from .env by Load when present).
func Credentials() (username, password string) {
	return os.Getenv("KURRENTDB_USERNAME"), os.Getenv("KURRENTDB_PASSWORD")
}

// braceVarPattern matches the braced ${VAR} interpolation form only. A
// bare `$` (e.g. inside an inline connection-string password) is left
// untouched.
var braceVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// Expand substitutes ${VAR} references in s with their value from the
// process environment (which includes .env after Load). Returns an
// error naming any referenced variable that isn't set, so a missing
// secret fails loudly instead of silently expanding to an empty
// string.
func Expand(s string) (string, error) {
	var missing []string
	out := braceVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-1]
		if v, ok := os.LookupEnv(name); ok {
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
