package config

// Diagnostic codes for the loose validation path used by Describe
// (which feeds the LSP server). Format is `<scope>.<rule>` with
// hyphenated rule names per the LSP plan's Decision 5; values
// land on `Diagnostic.Rule` and travel out via the LSP server's
// `Diagnostic.code` field.
//
// Loose vs strict parity: Load also enforces engine_version
// validity and a few related checks. Those don't have rule codes
// here yet because they rarely surface in practice and the strict
// run-time error is actionable enough. Add codes when LSP UX
// coverage becomes important.
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

// Description is the LSP-shaped view of a parsed gaffer config
// file: the file path, format, projections (with source ranges and
// resolved entry/fixture paths), plus any file-level diagnostics
// (parse errors). The LSP server iterates this to emit code lenses
// and diagnostics.
//
// "Loose" semantics: parse failures and per-element issues become
// diagnostics rather than aborting the whole call, so the editor
// can still show partial state. Compare with strict Load which
// returns the first error.
//
// JSON tags are present so the LSP server can marshal directly
// without an intermediate transform; `omitempty` on optional
// fields keeps the wire shape stable across V1's narrow surface
// and any future fields.
type Description struct {
	ConfigFile string `json:"configFile"`
	Format     string `json:"format"`
	// Connection is the default env's connection (the [env.*] block
	// with default = true). Empty string means no default env was
	// declared; the editor uses this to gate the "live" option in
	// the run-projection picker.
	Connection string `json:"connection,omitempty"`
	// Environments lists the configured [env.<name>] blocks, sorted by
	// name, each flagged if it's the default. The editor uses these to
	// populate the debug source picker and to decide whether a one-click
	// Debug has an unambiguous live target.
	Environments []EnvDescription        `json:"environments,omitempty"`
	Projections  []ProjectionDescription `json:"projections,omitempty"`
	Diagnostics  []Diagnostic            `json:"diagnostics,omitempty"`
}

// EnvDescription is a single [env.<name>] block in a Description: its
// name and whether it's the default (the env used when --env is
// omitted). The connection string is deliberately omitted - the editor
// selects an env by name and lets the CLI resolve the connection (and
// any ${VAR} / .env.<name> credentials) at launch.
type EnvDescription struct {
	Name    string `json:"name"`
	Default bool   `json:"default,omitempty"`
}

// ProjectionDescription is a single projection's view: name, entry
// path (raw + resolved), source range of its [[projection]] header,
// per-fixture details, plus an optional projection-level diagnostic
// (missing name, escaping entry path, duplicate name).
//
// Strict-only fields on Projection (ExecutionTimeout, EngineVersion
// overrides) are intentionally absent here - they don't drive any V1
// lens or diagnostic. Add when hover-on-projection or similar UX
// surfaces them.
type ProjectionDescription struct {
	Name         string               `json:"name"`
	Entry        string               `json:"entry"`
	EntryAbsPath string               `json:"entryAbsPath,omitempty"`
	Range        SourceRange          `json:"range"`
	Fixtures     []FixtureDescription `json:"fixtures,omitempty"`
	Diagnostic   *Diagnostic          `json:"diagnostic,omitempty"`
}

// FixtureDescription is a single fixture's view: name, path (raw +
// resolved), source range (the `fixtures.<name>` line if the
// dotted-key form was used, else the projection header range), and
// a diagnostic when invalid. A fixture is valid iff Diagnostic ==
// nil; AbsPath is populated only when valid.
type FixtureDescription struct {
	Name       string      `json:"name"`
	Path       string      `json:"path"`
	AbsPath    string      `json:"absPath,omitempty"`
	Range      SourceRange `json:"range"`
	Diagnostic *Diagnostic `json:"diagnostic,omitempty"`
}

// Diagnostic carries a rule code (machine-readable) plus a
// human-readable message and the source range to anchor on.
type Diagnostic struct {
	Range   SourceRange `json:"range"`
	Rule    string      `json:"rule"`
	Message string      `json:"message"`
}

// SourceRange is a 1-indexed (line) / 0-indexed (column)
// half-open range matching the LSP convention. Single-line ranges
// have StartLine == EndLine.
//
// Columns are byte offsets, not UTF-16 code units. Sufficient for
// ASCII content (which is most of the surface here - bare TOML
// keys and short paths). For non-ASCII content the wire emission
// over-reports the column by some amount, but editors clamp to
// line length so the visible result is correct - the diagnostic /
// lens just covers a few characters more than necessary. Revisit
// when in-line ranges (token-level squiggles) land; not relevant
// for V1's full-line ranges.
type SourceRange struct {
	StartLine int `json:"startLine"`
	StartCol  int `json:"startCol"`
	EndLine   int `json:"endLine"`
	EndCol    int `json:"endCol"`
}
