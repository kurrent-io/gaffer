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
	"os"
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
	// AuthHost is the normalized endpoint set the connection names (see
	// authHost); tokens for this target are stored and looked up bound to
	// it, never crossing to another host. Set only when OAuth is set.
	AuthHost string
	// BearerToken lazily yields an OAuth access token for the target, set
	// only when OAuth is configured. It resolves the process-shared token
	// source (SharedTokenSource) so the connection and this background read
	// refresh through one source rather than racing two over the same stored
	// refresh token. A missing or locked token surfaces as *AuthRequiredError
	// (oauth.ErrNoToken / ErrKeyringLocked underneath). Unlike the connection's
	// own provider it never deletes a stored token - credential lifecycle stays
	// with the connection that owns re-sign-in - though it does evict the shared
	// source on an invalid_grant so a re-`gaffer auth` is seen next time. The
	// store opens interactively (as the connection's does), so on a terminal a
	// passphrase prompt is possible; a protocol server's non-tty stdin suppresses
	// it and fails closed instead. The wait is bounded by ctx; the underlying
	// fetch is independently bounded by the OAuth client's own timeout.
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
	// A base .env that exists but was never loaded means the process-env
	// fallback would silently resolve empty credentials - the UI-1820
	// shape, from a future entry point that forgets the startup Load.
	// Refuse loudly at the choke point instead.
	if !envvar.Loaded(root) {
		if _, err := os.Stat(filepath.Join(root, ".env")); err == nil {
			return Target{}, fmt.Errorf("a .env exists at %q but was never loaded; call envvar.Load at startup before resolving targets", root)
		}
	}
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
		// An unparseable connection means there is no host to bind a token
		// to, so an OAuth target refuses to resolve rather than fall back to
		// host-unbound credentials.
		if t.AuthHost, err = authHost(conn); err != nil {
			return Target{}, fmt.Errorf("resolving OAuth host binding: %w", err)
		}
		t.OAuthClientSecret = envvar.OAuthClientSecret(overlay)
		t.OAuthCAFile = oauth.ResolveCAFile(env.OAuth.CAFile, root)
		t.BearerToken = bearerSource(env.Name, env.OAuth, t.OAuthCAFile, t.OAuthClientSecret)
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

// ExpandConnection returns the env's ${VAR}-expanded connection string alone.
// For consumers that need the dial string but must not inherit Resolve's
// refusals - e.g. scrubbing a panic message against an env whose OAuth host
// binding won't resolve: expansion still succeeds there, so the secret the
// dial saw still gets masked.
func ExpandConnection(root string, env config.ResolvedEnv) (string, error) {
	overlay, err := envvar.Overlay(root, env.Name)
	if err != nil {
		return "", err
	}
	return envvar.Expand(env.Connection, overlay)
}

// oauthTimeout bounds OIDC discovery and each token fetch/refresh, so a slow
// or unreachable identity provider can't hang a caller. The single bound for
// every token source built here (the connection's provider included).
const oauthTimeout = 30 * time.Second

// newTokenSource is the one construction path for a target's OAuth token
// source: OIDC discovery over a timeout-bounded client (verifying the issuer
// against caFile when set), the client-credentials grant when a secret is
// configured, else the token store. The store always opens interactively: a
// connection may prompt for the keyring passphrase, and the terminal check in
// the file-keyring password hook already suppresses a prompt where none can
// happen (a protocol server's non-tty stdin), so a background reader sharing
// this source can't force a prompt that wouldn't already fire. Callers reach
// this through SharedTokenSource, which shares one instance per identity;
// deleting a rejected stored token is handled separately by InvalidateTokenSource.
func newTokenSource(c *config.OAuthConfig, caFile, secret string) (oauth2.TokenSource, error) {
	var store *oauth.TokenStore
	if secret == "" {
		dir, err := userconfig.DefaultDir()
		if err != nil {
			return nil, err
		}
		if store, err = oauth.OpenTokenStore(dir); err != nil {
			return nil, err
		}
	}
	// Background, not a per-call context: the source outlives any single
	// request; the timeout-bearing client bounds discovery and refresh.
	ctx, err := oauth.WithHTTPClient(context.Background(), oauthTimeout, caFile)
	if err != nil {
		return nil, err
	}
	return oauth.TokenSource(ctx, oauth.Config{
		Issuer:   c.Issuer,
		ClientID: c.ClientID,
		Scopes:   c.Scopes,
		Audience: c.Audience,
	}, secret, store)
}

// bearerSource builds the lazy token accessor for Target.BearerToken. It
// resolves the process-shared token source (SharedTokenSource) on each call so
// a rebuild after an eviction is picked up, keeping Resolve itself free of
// keyring and network I/O. Unlike the connection's provider it never deletes a
// stored token - credential lifecycle stays with the connection that owns
// re-sign-in - but it does evict the shared source on an invalid_grant so a
// re-`gaffer auth` is seen next time.
func bearerSource(env string, c *config.OAuthConfig, caFile, secret string) func(context.Context) (string, error) {
	id := oauth.Identity(c.Issuer, c.ClientID)
	// A stored-token source is resolved from the shared cache on every call so
	// an eviction (a re-`gaffer auth`) is picked up. A client-credentials source
	// isn't cached (no refresh token to serialize), so memoize it here to avoid
	// re-running OIDC discovery on each call.
	var (
		once  sync.Once
		ccSrc oauth2.TokenSource
		ccErr error
	)
	resolve := func() (oauth2.TokenSource, error) {
		if secret == "" {
			return SharedTokenSource(env, c, caFile, secret)
		}
		once.Do(func() { ccSrc, ccErr = SharedTokenSource(env, c, caFile, secret) })
		return ccSrc, ccErr
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
			src, err := resolve()
			if err != nil {
				ch <- result{"", err}
				return
			}
			tok, err := src.Token()
			if err != nil {
				if secret == "" && oauth.IsInvalidGrant(err) {
					EvictTokenSource(id)
				}
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
