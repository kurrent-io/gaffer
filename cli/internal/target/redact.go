package target

import "strings"

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
// ScrubConnection replaces every occurrence of the raw connection string in
// msg with its redacted form - for cleaning errors from libraries that echo
// their input verbatim before the message reaches a user or an MCP envelope.
func ScrubConnection(msg, connStr string) string {
	return scrubRaw(msg, connStr, RedactConnection(connStr))
}

func scrubRaw(msg, raw, redacted string) string {
	if raw == "" || raw == redacted {
		return msg
	}
	return strings.ReplaceAll(msg, raw, redacted)
}
