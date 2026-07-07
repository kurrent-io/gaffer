package engine

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/envvar"
	"github.com/kurrent-io/gaffer/cli/internal/oauth"
	"github.com/kurrent-io/gaffer/cli/internal/target"
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

// AuthInvalidation is tripped by the OAuth credentials provider when the IdP
// rejects the stored token (invalid_grant), after it clears the dead token.
// Connect returns it (nil for non-OAuth envs) so the live source can turn the
// resulting connection failure into an AuthRequiredError - prompting re-sign-in
// rather than reporting a generic disconnect. The provider runs on the client's
// RPC goroutines, so access is atomic.
type AuthInvalidation struct{ tripped atomic.Bool }

func (a *AuthInvalidation) Trip()         { a.tripped.Store(true) }
func (a *AuthInvalidation) Tripped() bool { return a.tripped.Load() }

// Connect dials the environment. It takes the selected env whole (rather
// than exploded fields) so a new auth-relevant field on ResolvedEnv can't be
// silently forgotten at a call site.
func Connect(projectRoot string, env config.ResolvedEnv) (*kurrentdb.Client, *AuthInvalidation, error) {
	// Base .env is also loaded once at startup; reloading here (no-override,
	// so it never clobbers shell vars) keeps Connect self-contained for
	// callers and tests that reach it without the startup path.
	if err := envvar.Load(projectRoot); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrDBConnect, err)
	}

	// All resolution - overlay, ${VAR} expansion, credential precedence,
	// cert paths, OAuth secret - happens in one place (see internal/target).
	tgt, err := target.Resolve(projectRoot, env)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrDBConnect, err)
	}

	redacted := target.RedactConnection(tgt.Connection)
	dbConfig, err := kurrentdb.ParseConnectionString(tgt.Connection)
	if err != nil {
		// Don't %w the underlying error: url.Parse errors echo the
		// original input, which for malformed connection strings
		// includes the password verbatim.
		return nil, nil, fmt.Errorf("%w: invalid connection string %s: %s", ErrDBConnect, redacted, target.ScrubConnection(err.Error(), tgt.Connection))
	}

	// authInvalidated is tripped by the OAuth provider if the IdP rejects the
	// stored token; the live source reads it to prompt re-sign-in. Nil unless
	// the env uses OAuth.
	var authInvalidated *AuthInvalidation

	// An env with OAuth configured uses it exclusively: KURRENTDB_USERNAME /
	// KURRENTDB_PASSWORD (and any inline user:pass in the connection string)
	// are intentionally ignored in favour of bearer tokens.
	if tgt.OAuth != nil {
		authInvalidated = &AuthInvalidation{}
		provider, err := oauthProvider(tgt, authInvalidated)
		if err != nil {
			// An auth-required error stands on its own: it asks the user to sign
			// in, not to debug a connection, so it isn't wrapped as ErrDBConnect.
			var authErr *AuthRequiredError
			if errors.As(err, &authErr) {
				return nil, nil, err
			}
			return nil, nil, fmt.Errorf("%w: %w", ErrDBConnect, err)
		}
		dbConfig.CredentialsProvider = provider
	} else if tgt.Username != "" {
		dbConfig.Username = tgt.Username
		dbConfig.Password = tgt.Password
	}

	// An X.509 user certificate is presented in the TLS handshake, so it's set
	// independently of the credentials branch above (an env may use mutual TLS
	// and OAuth together). The paths were resolved against the project root by
	// target.Resolve; the client would resolve them against its own cwd.
	if env.Cert != nil {
		if dbConfig.DisableTLS {
			return nil, nil, fmt.Errorf("%w: env %q sets a user certificate but the connection disables TLS; a user certificate requires TLS", ErrDBConnect, env.Name)
		}
		dbConfig.UserCertFile = tgt.CertFile
		dbConfig.UserKeyFile = tgt.KeyFile
	}
	dbConfig.Logger = kurrentdb.NoopLogging()

	client, err := kurrentdb.NewClient(dbConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s", ErrDBConnect, target.ScrubConnection(err.Error(), tgt.Connection))
	}
	return client, authInvalidated, nil
}

// Principal best-effort reports the identity gaffer authenticates as for an env,
// for attribution (the deploy ledger's actor). It mirrors the credential
// precedence in Connect: an OAuth env uses the client-credentials grant, so the
// principal is the client_id (the service identity); otherwise basic auth, whose
// username is KURRENTDB_USERNAME (the .env overlay) or the connection string's
// userinfo. A cert-only or anonymous env, or any resolution failure, yields "" -
// attribution is best-effort and never blocks a deploy.
func Principal(projectRoot string, env config.ResolvedEnv) string {
	if env.OAuth != nil {
		return env.OAuth.ClientID
	}
	if err := envvar.Load(projectRoot); err != nil {
		return ""
	}
	tgt, err := target.Resolve(projectRoot, config.ResolvedEnv{Name: env.Name, Connection: env.Connection})
	if err != nil {
		return ""
	}
	if tgt.Username != "" {
		return tgt.Username
	}
	dbConfig, err := kurrentdb.ParseConnectionString(tgt.Connection)
	if err != nil {
		return ""
	}
	return dbConfig.Username
}

// oauthProvider builds the KurrentDB credentials provider for a target's
// OAuth config. Construction goes through target.NewTokenSource - the single
// path shared with Target.BearerToken - with the interactive store opener:
// a connection may prompt for the keyring passphrase where a background read
// must not. The provider adds what the connection alone owns: mapping the
// needs-sign-in sentinels to AuthRequiredError, and dropping a rejected
// token + tripping the invalidation flag on invalid_grant.
func oauthProvider(tgt target.Target, authInvalidated *AuthInvalidation) (kurrentdb.CredentialsProvider, error) {
	c := tgt.OAuth
	src, store, err := target.NewTokenSource(c, tgt.OAuthCAFile, tgt.OAuthClientSecret, oauth.OpenTokenStore)
	if err != nil {
		// No stored token, or a passphrase-locked keyring we can't unlock
		// non-interactively: both need an interactive sign-in.
		if errors.Is(err, oauth.ErrNoToken) || errors.Is(err, oauth.ErrKeyringLocked) {
			return nil, &AuthRequiredError{Env: tgt.Env}
		}
		return nil, err
	}

	id := oauth.Identity(c.Issuer, c.ClientID)
	return func(context.Context) (*kurrentdb.Credentials, error) {
		tok, err := src.Token()
		if err != nil {
			// A rejected token (invalid_grant) can't be refreshed - it's a dead
			// credential, not a transient failure. Drop it from the store and
			// trip the flag so the run prompts re-sign-in rather than reporting
			// a generic disconnect. Only meaningful for the interactive flow
			// (store != nil); client-credentials has no stored token.
			//
			// The Delete is best-effort: the trip already drives the re-sign-in,
			// and the subsequent `gaffer auth` overwrites the token, so a failed
			// delete still self-heals.
			if store != nil && oauth.IsInvalidGrant(err) {
				_ = store.Delete(id)
				authInvalidated.Trip()
			}
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
