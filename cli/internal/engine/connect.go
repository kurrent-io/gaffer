package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/envvar"
	"github.com/kurrent-io/gaffer/cli/internal/oauth"
	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

// ErrDBConnect wraps every Connect failure (bad URL, dotenv read,
// kurrentdb client construction). Callers use errors.Is to classify
// the outcome for telemetry without pattern-matching on formatted
// error strings.
var ErrDBConnect = errors.New("connect to KurrentDB")

// ErrDBDisconnect wraps subscription-drop errors from a previously
// healthy connection. Surfaced by the live-source loop so callers can
// distinguish "couldn't connect" from "connected then lost the link".
var ErrDBDisconnect = errors.New("KurrentDB connection lost")

// AuthRequiredError is returned when an OAuth env can't authenticate without an
// interactive sign-in: no stored token, or a passphrase-locked keyring that
// can't be unlocked non-interactively. It's distinct from ErrDBConnect so
// callers (the --json stream, telemetry) can surface a "sign in" action rather
// than a generic connection failure. Env is the environment to authenticate.
type AuthRequiredError struct {
	Env string
}

func (e *AuthRequiredError) Error() string {
	return fmt.Sprintf("env %q requires sign-in: run `gaffer auth --env %s`", e.Env, e.Env)
}

func Connect(connStr, projectRoot, envName string, oauthCfg *config.OAuthConfig, certCfg *config.CertAuth) (*kurrentdb.Client, error) {
	// Base .env is also loaded once at startup; reloading here (no-override,
	// so it never clobbers shell vars) keeps Connect self-contained for
	// callers and tests that reach it without the startup path.
	if err := envvar.Load(projectRoot); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrDBConnect, err)
	}

	// Read the .env.<envName> overlay once and share it across connection
	// expansion and credential resolution, so both see the same view and
	// the file is parsed a single time.
	overlay, err := envvar.Overlay(projectRoot, envName)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrDBConnect, err)
	}

	// Interpolate ${VAR} (e.g. credentials kept out of the committed
	// connection) before parsing; a missing var errors here rather than
	// dialing a malformed endpoint. The overlay layers .env.<envName>
	// over the base .env for this target.
	connStr, err = envvar.Expand(connStr, overlay)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrDBConnect, err)
	}

	redacted := RedactConnection(connStr)
	dbConfig, err := kurrentdb.ParseConnectionString(connStr)
	if err != nil {
		// Don't %w the underlying error: url.Parse errors echo the
		// original input, which for malformed connection strings
		// includes the password verbatim.
		return nil, fmt.Errorf("%w: invalid connection string %s: %s", ErrDBConnect, redacted, scrubRaw(err.Error(), connStr, redacted))
	}

	// An env with OAuth configured uses it exclusively: KURRENTDB_USERNAME /
	// KURRENTDB_PASSWORD (and any inline user:pass in the connection string)
	// are intentionally ignored in favour of bearer tokens.
	if oauthCfg != nil {
		provider, err := oauthProvider(oauthCfg, envName, projectRoot, overlay)
		if err != nil {
			// An auth-required error stands on its own: it asks the user to sign
			// in, not to debug a connection, so it isn't wrapped as ErrDBConnect.
			var authErr *AuthRequiredError
			if errors.As(err, &authErr) {
				return nil, err
			}
			return nil, fmt.Errorf("%w: %s", ErrDBConnect, err)
		}
		dbConfig.CredentialsProvider = provider
	} else if username, password := envvar.Credentials(overlay); username != "" {
		dbConfig.Username = username
		dbConfig.Password = password
	}

	// An X.509 user certificate is presented in the TLS handshake, so it's set
	// independently of the credentials branch above (an env may use mutual TLS
	// and OAuth together). The client resolves relative cert paths against its
	// own cwd; resolving here against the project root - and ${VAR}-expanding,
	// like the connection string - keeps cert paths working from any directory.
	if certCfg != nil {
		if dbConfig.DisableTLS {
			return nil, fmt.Errorf("%w: env %q sets a user certificate but the connection disables TLS; a user certificate requires TLS", ErrDBConnect, envName)
		}
		certFile, err := resolveCertPath(certCfg.CertFile, projectRoot, overlay)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrDBConnect, err)
		}
		keyFile, err := resolveCertPath(certCfg.KeyFile, projectRoot, overlay)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrDBConnect, err)
		}
		// Guard against a ${VAR} that expands to empty: a half-set pair would
		// silently disable the cert (the client loads it only when both are set)
		// rather than authenticating as intended.
		if certFile == "" || keyFile == "" {
			return nil, fmt.Errorf("%w: env %q user certificate path resolved to empty (check ${VAR} expansion)", ErrDBConnect, envName)
		}
		dbConfig.UserCertFile = certFile
		dbConfig.UserKeyFile = keyFile
	}
	dbConfig.Logger = kurrentdb.NoopLogging()

	client, err := kurrentdb.NewClient(dbConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrDBConnect, scrubRaw(err.Error(), connStr, redacted))
	}
	return client, nil
}

// resolveCertPath expands ${VAR} references in a cert path (using the same
// overlay as the connection string) and resolves a relative result against the
// project root. An absolute path or empty string is returned unchanged.
func resolveCertPath(path, projectRoot string, overlay map[string]string) (string, error) {
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
	return filepath.Join(projectRoot, expanded), nil
}

// oauthTimeout bounds OIDC discovery and each token fetch/refresh, so a slow
// or unreachable identity provider can't hang a connection or an RPC.
const oauthTimeout = 30 * time.Second

// oauthProvider builds the KurrentDB credentials provider for an env's OAuth
// config. A configured client secret (KURRENTDB_OAUTH_CLIENT_SECRET) selects
// the client-credentials grant; otherwise the token stored by `gaffer auth` is
// used and refreshed in place.
func oauthProvider(c *config.OAuthConfig, envName, projectRoot string, overlay map[string]string) (kurrentdb.CredentialsProvider, error) {
	secret := envvar.OAuthClientSecret(overlay)

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

	// Background, not a per-RPC context: the token source outlives any single
	// request and refreshes on its own schedule. The timeout-bearing client
	// bounds discovery and refresh HTTP, and verifies the issuer against the
	// configured CA when one is set.
	ctx, err := oauth.WithHTTPClient(context.Background(), oauthTimeout, oauth.ResolveCAFile(c.CAFile, projectRoot))
	if err != nil {
		return nil, err
	}
	src, err := oauth.TokenSource(ctx, oauth.Config{
		Issuer:   c.Issuer,
		ClientID: c.ClientID,
		Scopes:   c.Scopes,
		Audience: c.Audience,
	}, secret, store)
	if err != nil {
		// No stored token, or a passphrase-locked keyring we can't unlock
		// non-interactively: both need an interactive sign-in.
		if errors.Is(err, oauth.ErrNoToken) || errors.Is(err, oauth.ErrKeyringLocked) {
			return nil, &AuthRequiredError{Env: envName}
		}
		return nil, err
	}

	return func(context.Context) (*kurrentdb.Credentials, error) {
		tok, err := src.Token()
		if err != nil {
			return nil, err
		}
		return &kurrentdb.Credentials{BearerToken: tok.AccessToken}, nil
	}, nil
}

// dbVersionUnknown is the value telemetry stamps on db_version when
// the server probe returns an error or a zero version. Matches the
// schema convention - the field is omitted when not connected and is
// "unknown" when we couldn't parse what the server reported.
const dbVersionUnknown = "unknown"

// serverVersionProvider abstracts kurrentdb.Client's GetServerVersion
// so ProbeServerVersion can be unit-tested without a live database.
// *kurrentdb.Client satisfies it.
type serverVersionProvider interface {
	GetServerVersion() (*kurrentdb.ServerVersion, error)
}

// ProbeServerVersion asks the connected client for the server's
// major.minor version. Returns "unknown" on probe failure or a zero
// version, never an error: telemetry is best-effort and a missing
// version shouldn't fail the run.
//
// The kurrentdb client caches the version internally after first
// connection, so calling this is a cheap accessor.
func ProbeServerVersion(client serverVersionProvider) string {
	v, err := client.GetServerVersion()
	if err != nil || v == nil || (v.Major == 0 && v.Minor == 0) {
		return dbVersionUnknown
	}
	return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}

// RedactConnection masks the password portion of a KurrentDB connection
// string, leaving the scheme, username, host, port, and path intact.
//
//	"kurrentdb+discover://admin:supersecret@host:2113/p" ->
//	"kurrentdb+discover://admin:***@host:2113/p"
//
// String-walk implementation rather than url.Parse: net/url percent-encodes
// the mask, and we want this to work for malformed inputs (which is exactly
// when we most need redaction, since parser errors echo the input).
func RedactConnection(connStr string) string {
	const sep = "://"
	schemeIdx := strings.Index(connStr, sep)
	if schemeIdx < 0 {
		return connStr
	}
	authStart := schemeIdx + len(sep)
	rest := connStr[authStart:]

	end := len(rest)
	for i, c := range rest {
		if c == '/' || c == '?' || c == '#' {
			end = i
			break
		}
	}
	authority := rest[:end]
	at := strings.LastIndex(authority, "@")
	if at < 0 {
		return connStr
	}
	userinfo := authority[:at]
	colon := strings.Index(userinfo, ":")
	if colon < 0 {
		return connStr
	}
	return connStr[:authStart] + userinfo[:colon+1] + "***" + connStr[authStart+at:]
}

// scrubRaw replaces every occurrence of the raw connection string with its
// redacted form. Used to clean error messages from libraries (e.g. url.Parse)
// that echo the input verbatim.
func scrubRaw(msg, raw, redacted string) string {
	if raw == "" || raw == redacted {
		return msg
	}
	return strings.ReplaceAll(msg, raw, redacted)
}
