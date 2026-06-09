// Package envvar is the shared environment-variable access layer:
// loading the project's .env into the process environment (Load),
// reading well-known values (Credentials), interpolating ${VAR}
// references (Expand), and parsing truthy flags (IsTruthy). Centralised
// here so .env is a uniform underlay for every env-var read - telemetry
// opt-out, update-check, credentials, connection strings - rather than
// being loaded ad hoc on one code path.
package envvar

import "strings"

// IsTruthy reports whether v is a recognised "on" value: "1", "true",
// "yes", or "on" (case-insensitive, whitespace-trimmed). Anything else
// is false, including the empty string and "0" / "false" / "no" /
// "off". Matches the convention KurrentDB's CLI uses for its
// KURRENTDB_TELEMETRY_OPTOUT variable so cross-product opt-outs feel
// the same.
func IsTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
