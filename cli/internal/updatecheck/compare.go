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

// IsDevVersion reports whether v is a local dev build - a semver
// pre-release such as `0.3.1-dev`. Dev builds come from source, not from
// npm, so the update check skips them: comparing `0.3.1-dev` against the
// published `0.3.1` would otherwise flag the release as "newer" (semver
// sorts a pre-release below its release) on every run.
func IsDevVersion(v string) bool {
	n := normalizeSemver(v)
	return semver.IsValid(n) && semver.Prerelease(n) != ""
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
