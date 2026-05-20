package config

import (
	"fmt"
	"path"
	"strings"
)

// Shared validation predicates for the strict (Load) and loose
// (Describe) paths. Each predicate returns a rule code, a
// human-readable message, and a bool flag - callers wrap the
// message into either a strict error or a loose Diagnostic, but
// the rule list and ordering live in one place so the two paths
// can't drift.
//
// Engine-version checks live in validate() rather than here because
// the loose path doesn't need them yet. Add when LSP UX justifies.

// checkProjection returns the first failing projection-level rule,
// in priority order: missing name, missing entry, entry escapes
// root. Duplicate-name detection is cross-projection state and
// belongs in the caller, not here.
func checkProjection(p Projection) (rule, message string, ok bool) {
	if p.Name == "" {
		return RuleProjectionMissingName,
			"projection missing required field: name",
			true
	}
	if p.Entry == "" {
		return RuleProjectionMissingEntry,
			fmt.Sprintf("projection %q missing required field: entry", p.Name),
			true
	}
	if escapesRoot(p.Entry) {
		return RuleProjectionEntryEscapesRoot,
			fmt.Sprintf("projection %q entry must not escape project root: %s", p.Name, p.Entry),
			true
	}
	return "", "", false
}

// checkFixture returns the first failing fixture-level rule, in
// priority order: empty name, empty path, path escapes root.
// `projection` is for message formatting only - rules don't depend
// on which projection owns the fixture.
func checkFixture(projection, name, fixturePath string) (rule, message string, ok bool) {
	if name == "" {
		return RuleFixtureEmptyName,
			fmt.Sprintf("projection %q has a fixture with an empty name", projection),
			true
	}
	if fixturePath == "" {
		return RuleFixtureEmptyPath,
			fmt.Sprintf("projection %q fixture %q has empty path", projection, name),
			true
	}
	if escapesRoot(fixturePath) {
		return RuleFixturePathEscapesRoot,
			fmt.Sprintf("projection %q fixture %q path must not escape project root: %s", projection, name, fixturePath),
			true
	}
	return "", "", false
}

// escapesRoot reports whether a slash-form relative path resolves
// outside the project. Toml entries are stored slash-form by
// scaffold; using path.Clean (always-slash) here keeps the check
// platform-stable. Matches scaffold.validateRelPath's rule so a
// successful scaffold can never produce a toml that later fails
// Load.
//
// Critically, only proper traversal counts as escape - a literal
// filename like "..hidden.js" stays inside the project and must
// not be rejected.
func escapesRoot(rel string) bool {
	cleaned := path.Clean(strings.ReplaceAll(rel, "\\", "/"))
	return cleaned == ".." || strings.HasPrefix(cleaned, "../")
}
