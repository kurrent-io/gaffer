package engine

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/dotenv"
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

func Connect(connStr, projectRoot string) (*kurrentdb.Client, error) {
	if err := dotenv.Load(projectRoot, ""); err != nil {
		return nil, fmt.Errorf("%w: loading .env: %s", ErrDBConnect, err)
	}

	redacted := RedactConnection(connStr)
	dbConfig, err := kurrentdb.ParseConnectionString(connStr)
	if err != nil {
		// Don't %w the underlying error: url.Parse errors echo the
		// original input, which for malformed connection strings
		// includes the password verbatim.
		return nil, fmt.Errorf("%w: invalid connection string %s: %s", ErrDBConnect, redacted, scrubRaw(err.Error(), connStr, redacted))
	}

	username, password := dotenv.Credentials()
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
