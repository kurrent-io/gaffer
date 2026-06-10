package engine

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/envvar"
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

func Connect(connStr, projectRoot, envName string) (*kurrentdb.Client, error) {
	// Base .env is also loaded once at startup; reloading here (no-override,
	// so it never clobbers shell vars) keeps Connect self-contained for
	// callers and tests that reach it without the startup path.
	if err := envvar.Load(projectRoot); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrDBConnect, err)
	}

	// Interpolate ${VAR} (e.g. credentials kept out of the committed
	// connection) before parsing; a missing var errors here rather than
	// dialing a malformed endpoint. envName layers .env.<envName> over
	// the base .env for this target.
	connStr, err := envvar.Expand(connStr, projectRoot, envName)
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

	username, password := envvar.Credentials()
	if username != "" {
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
