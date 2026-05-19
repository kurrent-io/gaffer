// Package envvar holds tiny shared helpers for parsing environment
// variables. Lives in its own package so feature packages (telemetry,
// updatecheck) share the same notion of "truthy" without growing
// cross-package imports.
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
