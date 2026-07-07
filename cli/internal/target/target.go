// Package target resolves a KurrentDB environment into a ready-to-dial
// description: the expanded connection string and every credential source,
// derived once, in one place. UI-1820 existed because this resolution was
// re-implemented per consumer (connect, actor attribution, the config-drift
// check) with parity maintained by comments; anything that talks to the
// database resolves through here instead.
package target

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/envvar"
	"github.com/kurrent-io/gaffer/cli/internal/oauth"
	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
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
	// OAuthCAFile is the OAuth issuer's CA file anchored to the project
	// root (oauth.ResolveCAFile semantics - no ${VAR} expansion, matching
	// the connection's behaviour). Meaningful only when OAuth is set.
	OAuthCAFile string
	// BearerToken lazily yields an OAuth access token for the target, set
	// only when OAuth is configured. The token source (OIDC discovery, the
	// client-credentials grant or the token stored by `gaffer auth`) is
	// built on first call and memoized; safe for concurrent use. It never
	// prompts for gaffer's keyring passphrase - a missing or locked token
	// errors (oauth.ErrNoToken / ErrKeyringLocked; an OS keychain may
	// enforce its own policy) - and unlike the connection's own provider it
	// never deletes a stored token: credential lifecycle stays with the
	// connection that owns re-sign-in. The wait is bounded by ctx; the
	// underlying fetch is independently bounded by the OAuth client's own
	// timeout.
	BearerToken func(ctx context.Context) (string, error)
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
		t.OAuthCAFile = oauth.ResolveCAFile(env.OAuth.CAFile, root)
		t.BearerToken = bearerSource(env.OAuth, t.OAuthCAFile, t.OAuthClientSecret)
	} else {
		t.Username, t.Password = envvar.Credentials(overlay)
	}
	if env.Cert != nil {
		if t.CertFile, err = resolveCertPath(env.Cert.CertFile, root, overlay); err != nil {
			return Target{}, fmt.Errorf("resolving user certificate path: %w", err)
		}
		if t.KeyFile, err = resolveCertPath(env.Cert.KeyFile, root, overlay); err != nil {
			return Target{}, fmt.Errorf("resolving user certificate key path: %w", err)
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

// oauthTimeout bounds OIDC discovery and each token fetch/refresh, so a slow
// or unreachable identity provider can't hang a caller. The single bound for
// every token source built here (the connection's provider included).
const oauthTimeout = 30 * time.Second

// NewTokenSource is the one construction path for a target's OAuth token
// source: OIDC discovery over a timeout-bounded client (verifying the issuer
// against caFile when set), the client-credentials grant when a secret is
// configured, else the token store opened via openStore (interactive for the
// connection, non-interactive for background reads). The returned store is
// non-nil only on the stored-token path - the connection's provider uses it
// to drop a rejected token. Both the gRPC credentials provider and
// Target.BearerToken build through here so the two can't drift.
func NewTokenSource(c *config.OAuthConfig, caFile, secret string, openStore func(dir string) (*oauth.TokenStore, error)) (oauth2.TokenSource, *oauth.TokenStore, error) {
	var store *oauth.TokenStore
	if secret == "" {
		dir, err := userconfig.DefaultDir()
		if err != nil {
			return nil, nil, err
		}
		if store, err = openStore(dir); err != nil {
			return nil, nil, err
		}
	}
	// Background, not a per-call context: the source outlives any single
	// request; the timeout-bearing client bounds discovery and refresh.
	ctx, err := oauth.WithHTTPClient(context.Background(), oauthTimeout, caFile)
	if err != nil {
		return nil, nil, err
	}
	src, err := oauth.TokenSource(ctx, oauth.Config{
		Issuer:   c.Issuer,
		ClientID: c.ClientID,
		Scopes:   c.Scopes,
		Audience: c.Audience,
	}, secret, store)
	if err != nil {
		return nil, nil, err
	}
	return src, store, nil
}

// bearerSource builds the lazy token accessor for Target.BearerToken. The
// underlying oauth2 source is constructed once, on first call, so Resolve
// itself stays free of keyring and network I/O. The token store opens
// non-interactively: a background check must never prompt.
func bearerSource(c *config.OAuthConfig, caFile, secret string) func(context.Context) (string, error) {
	var (
		once    sync.Once
		src     oauth2.TokenSource
		initErr error
	)
	build := func() {
		src, _, initErr = NewTokenSource(c, caFile, secret, oauth.OpenTokenStoreNonInteractive)
	}
	return func(ctx context.Context) (string, error) {
		// oauth2 sources take no per-call context, so the caller's deadline
		// bounds the wait here; an abandoned fetch self-terminates within
		// the OAuth client's own timeout.
		type result struct {
			tok string
			err error
		}
		ch := make(chan result, 1)
		go func() {
			once.Do(build)
			if initErr != nil {
				ch <- result{"", initErr}
				return
			}
			tok, err := src.Token()
			if err != nil {
				ch <- result{"", err}
				return
			}
			ch <- result{tok.AccessToken, nil}
		}()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case r := <-ch:
			return r.tok, r.err
		}
	}
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
