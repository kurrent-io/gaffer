package target

import (
	"strconv"
	"strings"
)

// RedactConnection masks the password portion of a KurrentDB connection
// string, leaving the scheme, username, host, port, and path intact.
//
//	"kurrentdb+discover://admin:supersecret@host:2113/p" ->
//	"kurrentdb+discover://admin:***@host:2113/p"
//
// String-walk implementation rather than url.Parse: net/url percent-encodes
// the mask, and we want this to work for malformed inputs (which is exactly
// when we most need redaction, since parser errors echo the input). Inputs
// too malformed for the authority walk (a one-slash scheme, no scheme) fall
// back to masking any user:pass@ fragment found by connectionPassword.
func RedactConnection(connStr string) string {
	if masked, ok := redactAuthority(connStr); ok {
		return masked
	}
	if pw := connectionPassword(connStr); pw != "" {
		return strings.ReplaceAll(connStr, ":"+pw+"@", ":***@")
	}
	return connStr
}

// redactAuthority masks the password in a well-formed scheme://user:pass@host
// string. ok reports whether it found a password to mask.
func redactAuthority(connStr string) (string, bool) {
	const sep = "://"
	schemeIdx := strings.Index(connStr, sep)
	if schemeIdx < 0 {
		return "", false
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
		return "", false
	}
	userinfo := authority[:at]
	colon := strings.Index(userinfo, ":")
	if colon < 0 {
		return "", false
	}
	return connStr[:authStart] + userinfo[:colon+1] + "***" + connStr[authStart+at:], true
}

// connectionPassword best-effort extracts an inline password from a
// connection string, tolerating malformed inputs (a one-slash scheme, no
// scheme at all): those are exactly the strings whose parser errors echo
// fragments that full-string replacement can't match. The userinfo candidate
// is the text between the last '/' (or string start) and the last '@' before
// the query/fragment; the password follows its first ':'.
func connectionPassword(connStr string) string {
	head := connStr
	if i := strings.IndexAny(head, "?#"); i >= 0 {
		head = head[:i]
	}
	at := strings.LastIndex(head, "@")
	if at < 0 {
		return ""
	}
	userinfo := head[:at]
	if i := strings.LastIndex(userinfo, "/"); i >= 0 {
		userinfo = userinfo[i+1:]
	}
	_, pw, ok := strings.Cut(userinfo, ":")
	if !ok {
		return ""
	}
	return pw
}

// ScrubConnection replaces every echo of the connection string in msg with a
// redacted form - for cleaning errors from libraries that echo their input
// verbatim before the message reaches a user or an MCP envelope. Echoes come
// in more spellings than the raw string: url errors %q-quote their input
// (escaping backslashes, quotes, and control characters), and the kurrentdb
// parser echoes fragments (the URL path, a host segment) that no full-string
// replacement matches. So scrub the raw and %q-escaped whole string, then the
// password itself in its ":pw@" context, raw and %q-escaped.
func ScrubConnection(msg, connStr string) string {
	redacted := RedactConnection(connStr)
	msg = scrubRaw(msg, connStr, redacted)
	msg = scrubRaw(msg, quoteInner(connStr), quoteInner(redacted))
	if pw := connectionPassword(connStr); pw != "" {
		msg = scrubRaw(msg, ":"+pw+"@", ":***@")
		msg = scrubRaw(msg, ":"+quoteInner(pw)+"@", ":***@")
	}
	return msg
}

// quoteInner is strconv.Quote without the surrounding quotes: the spelling a
// %q-formatted echo embeds a string as.
func quoteInner(s string) string {
	q := strconv.Quote(s)
	return q[1 : len(q)-1]
}

func scrubRaw(msg, raw, redacted string) string {
	if raw == "" || raw == redacted {
		return msg
	}
	return strings.ReplaceAll(msg, raw, redacted)
}
