package config

// Diagnostic codes for the loose validation path used by Describe
// (which feeds the LSP server). Format is `<scope>.<rule>` with
// hyphenated rule names per the LSP plan's Decision 5; values
// land on `Diagnostic.code` in the eventual LSP server emission.
//
// The strict Load path uses formatted error messages instead of
// codes - it surfaces errors via stderr and parsers don't read the
// codes. Codes here apply only to the loose path.
const (
	// File-level: parser failed to decode the toml.
	RuleConfigParseError = "config.parse-error"
	// File-level: parser and source-position scanner disagree on
	// projection counts. Distinct from parse-error - the parser
	// succeeded - so consumers can offer different actions
	// (e.g. "report a bug" vs "fix your toml").
	RuleConfigScanDrift = "config.scan-drift"

	RuleProjectionMissingName      = "projection.missing-name"
	RuleProjectionMissingEntry     = "projection.missing-entry"
	RuleProjectionEntryEscapesRoot = "projection.entry-escapes-root"
	RuleProjectionDuplicateName    = "projection.duplicate-name"

	RuleFixtureEmptyName       = "fixture.empty-name"
	RuleFixtureEmptyPath       = "fixture.empty-path"
	RuleFixturePathEscapesRoot = "fixture.path-escapes-root"
)

// Note on loose-vs-strict parity: the loose Describe path covers the
// rules above. Strict Load (config.go validate()) additionally enforces
// engine_version validity and a few other version-related checks. Those
// don't have rule codes here yet because they rarely surface in
// practice and the strict run-time error is actionable enough. Add
// codes when LSP UX coverage becomes important.
