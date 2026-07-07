// Package target resolves a KurrentDB environment into a ready-to-dial
// description: the expanded connection string and every credential source,
// derived once, in one place. UI-1820 existed because this resolution was
// re-implemented per consumer (connect, actor attribution, the config-drift
// check) with parity maintained by comments; anything that talks to the
// database resolves through here instead.
package target

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/envvar"
)

// Target is the resolved description of a KurrentDB environment. Construct
// via Resolve; the zero value means "nothing to dial".
type Target struct {
	// Env is the environment name, "" for an ad-hoc --connection target.
	Env string
	// Connection is the ${VAR}-expanded connection string.
	Connection string
	// Username and Password are the basic credentials from the environment
	// files/shell (envvar.Credentials precedence), or "" when none are set.
	// They take precedence over the connection string's userinfo; a consumer
	// that can read userinfo (the kurrentdb client, the node's HTTP surface)
	// falls back to it when these are empty. Ignored on an OAuth env.
	Username string
	Password string
	// OAuth is the env's OAuth config; when set, basic credentials are
	// intentionally ignored in favour of bearer tokens.
	OAuth *config.OAuthConfig
	// OAuthClientSecret is the resolved KURRENTDB_OAUTH_CLIENT_SECRET; when
	// non-empty it selects the client-credentials grant over the interactive
	// token store. Meaningful only when OAuth is set.
	OAuthClientSecret string
	// CertFile and KeyFile are the env's X.509 user certificate paths,
	// ${VAR}-expanded and anchored to the project root. Both empty when the
	// env doesn't use certificate auth.
	CertFile string
	KeyFile  string
}

// Resolve derives the Target for an environment: the one place the
// envvar resolution stack (overlay + ${VAR} expansion + credential
// precedence) runs.
//
// Resolve never touches the process environment - the base .env is loaded
// once at startup (and by engine.Connect for self-contained use), so calling
// this from any goroutine is safe even while cgo sessions read environ.
func Resolve(root string, env config.ResolvedEnv) (Target, error) {
	overlay, err := envvar.Overlay(root, env.Name)
	if err != nil {
		return Target{}, fmt.Errorf("reading env overlay: %w", err)
	}
	conn, err := envvar.Expand(env.Connection, overlay)
	if err != nil {
		return Target{}, fmt.Errorf("expanding connection string: %w", err)
	}

	t := Target{
		Env:        env.Name,
		Connection: conn,
		OAuth:      env.OAuth,
	}
	if env.OAuth != nil {
		t.OAuthClientSecret = envvar.OAuthClientSecret(overlay)
	} else {
		t.Username, t.Password = envvar.Credentials(overlay)
	}
	if env.Cert != nil {
		if t.CertFile, err = resolveCertPath(env.Cert.CertFile, root, overlay); err != nil {
			return Target{}, err
		}
		if t.KeyFile, err = resolveCertPath(env.Cert.KeyFile, root, overlay); err != nil {
			return Target{}, err
		}
		// Guard against a ${VAR} that expands to empty: a half-set pair would
		// silently disable the cert (the client loads it only when both are
		// set) rather than authenticating as intended.
		if t.CertFile == "" || t.KeyFile == "" {
			return Target{}, fmt.Errorf("env %q user certificate path resolved to empty (check ${VAR} expansion)", env.Name)
		}
	}
	return t, nil
}

// resolveCertPath expands ${VAR} references in a cert path (using the same
// overlay as the connection string) and resolves a relative result against
// the project root. An absolute path or empty string is returned unchanged.
func resolveCertPath(path, root string, overlay map[string]string) (string, error) {
	expanded, err := envvar.Expand(path, overlay)
	if err != nil {
		return "", err
	}
	// Trim after expansion too: a ${VAR} can introduce surrounding whitespace
	// that the config-load trim never saw, which would corrupt the path.
	expanded = strings.TrimSpace(expanded)
	if expanded == "" || filepath.IsAbs(expanded) {
		return expanded, nil
	}
	return filepath.Join(root, expanded), nil
}
