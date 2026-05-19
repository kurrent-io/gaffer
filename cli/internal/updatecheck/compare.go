package updatecheck

import "golang.org/x/mod/semver"

// IsNewer reports whether latest is strictly greater than current under
// semver ordering. Both inputs accept either bare (`0.2.0`) or v-prefixed
// (`v0.2.0`) form; normalizeSemver adds the v when missing because
// golang.org/x/mod/semver requires it. Returns false on any malformed
// input - update-check is best-effort and must never block startup or
// produce a confusing notice.
func IsNewer(latest, current string) bool {
	l := normalizeSemver(latest)
	c := normalizeSemver(current)
	if !semver.IsValid(l) || !semver.IsValid(c) {
		return false
	}
	return semver.Compare(l, c) > 0
}

func normalizeSemver(v string) string {
	if v == "" {
		return ""
	}
	if v[0] == 'v' {
		return v
	}
	return "v" + v
}
