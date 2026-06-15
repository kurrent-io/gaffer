package engine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/envvar"
	"github.com/kurrent-io/gaffer/cli/internal/oauth"
	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
	"golang.org/x/oauth2"
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

func Connect(connStr, projectRoot, envName string, oauthCfg *config.OAuthConfig) (*kurrentdb.Client, error) {
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
		provider, err := oauthProvider(oauthCfg, envName, overlay)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrDBConnect, err)
		}
		dbConfig.CredentialsProvider = provider
	} else if username, password := envvar.Credentials(overlay); username != "" {
		dbConfig.Username = username
		dbConfig.Password = password
	}
	dbConfig.Logger = kurrentdb.NoopLogging()

	client, err := kurrentdb.NewClient(dbConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrDBConnect, scrubRaw(err.Error(), connStr, redacted))
	}
	return client, nil
}

// oauthTimeout bounds OIDC discovery and each token fetch/refresh, so a slow
// or unreachable identity provider can't hang a connection or an RPC.
const oauthTimeout = 30 * time.Second

// oauthProvider builds the KurrentDB credentials provider for an env's OAuth
// config. A configured client secret (KURRENTDB_OAUTH_CLIENT_SECRET) selects
// the client-credentials grant; otherwise the token stored by `gaffer auth` is
// used and refreshed in place.
func oauthProvider(c *config.OAuthConfig, envName string, overlay map[string]string) (kurrentdb.CredentialsProvider, error) {
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
	// bounds discovery and refresh HTTP.
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: oauthTimeout})
	src, err := oauth.TokenSource(ctx, oauth.Config{
		Issuer:   c.Issuer,
		ClientID: c.ClientID,
		Scopes:   c.Scopes,
		Audience: c.Audience,
	}, secret, store)
	if err != nil {
		if errors.Is(err, oauth.ErrNoToken) {
			return nil, fmt.Errorf("env %q requires an interactive login: run `gaffer auth --env %s`", envName, envName)
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
