package config

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// describeFile writes content to <dir>/gaffer.toml and returns the
// abs path. Test helper.
func describeFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	writeFile(t, path, content)
	return path
}

func TestDescribe_HappyPath(t *testing.T) {
	path := describeFile(t, `
quirks_version = "26.1.0"

[[projection]]
name = "checkout"
entry = "checkout.js"
engine_version = 2
fixtures.happy = "fixtures/orders.json"
fixtures.full = "fixtures/orders-full.json"
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if desc.Format != "toml" {
		t.Errorf("format: got %q want toml", desc.Format)
	}
	if desc.ConfigFile != path {
		t.Errorf("ConfigFile: got %q want %q", desc.ConfigFile, path)
	}
	if len(desc.Diagnostics) != 0 {
		t.Errorf("expected no file-level diagnostics, got %v", desc.Diagnostics)
	}
	if len(desc.Projections) != 1 {
		t.Fatalf("expected 1 projection, got %d", len(desc.Projections))
	}
	p := desc.Projections[0]
	if p.Name != "checkout" || p.Entry != "checkout.js" {
		t.Errorf("projection metadata wrong: %+v", p)
	}
	if !filepath.IsAbs(p.EntryAbsPath) {
		t.Errorf("EntryAbsPath should be absolute, got %q", p.EntryAbsPath)
	}
	if p.Diagnostic != nil {
		t.Errorf("expected no projection diagnostic, got %+v", p.Diagnostic)
	}
	// 4-line range: line 4 = [[projection]], starting col 0,
	// ending at the line's length.
	if p.Range.StartLine != 4 || p.Range.EndLine != 4 || p.Range.StartCol != 0 {
		t.Errorf("range: %+v", p.Range)
	}

	if len(p.Fixtures) != 2 {
		t.Fatalf("expected 2 fixtures, got %d", len(p.Fixtures))
	}
	// Alphabetical: full < happy.
	if p.Fixtures[0].Name != "full" || p.Fixtures[1].Name != "happy" {
		t.Errorf("fixtures not sorted: %v", []string{p.Fixtures[0].Name, p.Fixtures[1].Name})
	}
	for _, fx := range p.Fixtures {
		if fx.Diagnostic != nil {
			t.Errorf("expected valid fixture, got %+v", fx)
		}
	}
}

func TestDescribe_FixtureLineRangesPointAtTheirSourceLines(t *testing.T) {
	// Per-fixture range should be the `fixtures.<name>` line, not
	// the projection header.
	path := describeFile(t, `[[projection]]
name = "p"
entry = "p.js"
fixtures.happy = "fixtures/h.json"
fixtures.edge = "fixtures/e.json"
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	p := desc.Projections[0]
	// Sorted: edge first.
	if p.Fixtures[0].Name != "edge" || p.Fixtures[0].Range.StartLine != 5 {
		t.Errorf("edge range wrong: %+v", p.Fixtures[0])
	}
	if p.Fixtures[1].Name != "happy" || p.Fixtures[1].Range.StartLine != 4 {
		t.Errorf("happy range wrong: %+v", p.Fixtures[1])
	}
}

func TestDescribe_InlineTableFixturesFallBackToProjectionRange(t *testing.T) {
	// fixtures = { happy = "...", edge = "..." } has no per-key
	// source line. Per-fixture range falls back to the projection
	// header so a diagnostic still has SOMEWHERE to anchor.
	path := describeFile(t, `[[projection]]
name = "p"
entry = "p.js"
fixtures = { happy = "fixtures/h.json", edge = "fixtures/e.json" }
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	p := desc.Projections[0]
	if len(p.Fixtures) != 2 {
		t.Fatalf("expected 2 fixtures, got %d", len(p.Fixtures))
	}
	headerLine := p.Range.StartLine
	for _, fx := range p.Fixtures {
		if fx.Range.StartLine != headerLine {
			t.Errorf("fixture %q range %+v should fall back to header line %d", fx.Name, fx.Range, headerLine)
		}
	}
}

func TestDescribe_FixtureLinesScopedPerProjection(t *testing.T) {
	// Two projections both declaring `fixtures.happy` - the line
	// scanner sees both, but each fixture's range must point at
	// the line belonging to ITS projection, not the other.
	path := describeFile(t, `[[projection]]
name = "a"
entry = "a.js"
fixtures.happy = "fixtures/a.json"

[[projection]]
name = "b"
entry = "b.js"
fixtures.happy = "fixtures/b.json"
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(desc.Projections) != 2 {
		t.Fatalf("expected 2 projections")
	}
	if desc.Projections[0].Fixtures[0].Range.StartLine != 4 {
		t.Errorf("first projection's fixture should be on line 4, got %+v", desc.Projections[0].Fixtures[0].Range)
	}
	if desc.Projections[1].Fixtures[0].Range.StartLine != 9 {
		t.Errorf("second projection's fixture should be on line 9, got %+v", desc.Projections[1].Fixtures[0].Range)
	}
}

func TestDescribe_InvalidFixtureGetsDiagnosticWithRuleCode(t *testing.T) {
	path := describeFile(t, `[[projection]]
name = "p"
entry = "p.js"
fixtures.evil = "../escape.json"
fixtures.empty = ""
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	p := desc.Projections[0]
	if len(p.Fixtures) != 2 {
		t.Fatalf("expected 2 fixtures")
	}
	// Sorted: empty, evil.
	empty := p.Fixtures[0]
	if empty.Name != "empty" || empty.Diagnostic == nil {
		t.Fatalf("expected empty fixture invalid: %+v", empty)
	}
	if empty.Diagnostic.Rule != RuleFixtureEmptyPath {
		t.Errorf("rule: got %q want %q", empty.Diagnostic.Rule, RuleFixtureEmptyPath)
	}
	evil := p.Fixtures[1]
	if evil.Name != "evil" || evil.Diagnostic == nil {
		t.Fatalf("expected evil fixture invalid: %+v", evil)
	}
	if evil.Diagnostic.Rule != RuleFixturePathEscapesRoot {
		t.Errorf("rule: got %q want %q", evil.Diagnostic.Rule, RuleFixturePathEscapesRoot)
	}
}

func TestDescribe_EmptyFixtureNameDiagnostic(t *testing.T) {
	// Per the LSP plan: empty fixture name (`fixtures."" = "x"`) is
	// caught and surfaced. Strict Load rejects this; the loose
	// Describe path also rejects it via diagnostic so a future
	// quoted-empty-key submission lights up the editor.
	path := describeFile(t, `[[projection]]
name = "p"
entry = "p.js"
fixtures."" = "x.json"
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	p := desc.Projections[0]
	if len(p.Fixtures) != 1 {
		t.Fatalf("expected 1 fixture")
	}
	fx := p.Fixtures[0]
	if fx.Diagnostic == nil {
		t.Fatalf("expected invalid fixture, got %+v", fx)
	}
	if fx.Diagnostic.Rule != RuleFixtureEmptyName {
		t.Errorf("rule: got %q want %q", fx.Diagnostic.Rule, RuleFixtureEmptyName)
	}
}

func TestDescribe_ProjectionMissingName(t *testing.T) {
	path := describeFile(t, `[[projection]]
entry = "p.js"
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(desc.Projections) != 1 {
		t.Fatal("expected 1 projection slot")
	}
	d := desc.Projections[0].Diagnostic
	if d == nil || d.Rule != RuleProjectionMissingName {
		t.Fatalf("expected missing-name diagnostic, got %+v", d)
	}
}

func TestDescribe_ProjectionMissingEntry(t *testing.T) {
	path := describeFile(t, `[[projection]]
name = "p"
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	d := desc.Projections[0].Diagnostic
	if d == nil || d.Rule != RuleProjectionMissingEntry {
		t.Fatalf("expected missing-entry diagnostic, got %+v", d)
	}
}

func TestDescribe_ProjectionEntryEscapesRoot(t *testing.T) {
	path := describeFile(t, `[[projection]]
name = "p"
entry = "../escape.js"
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	d := desc.Projections[0].Diagnostic
	if d == nil || d.Rule != RuleProjectionEntryEscapesRoot {
		t.Fatalf("expected entry-escapes diagnostic, got %+v", d)
	}
}

func TestDescribe_ParseErrorBecomesFileLevelDiagnostic(t *testing.T) {
	// Syntactically broken TOML - we don't get projections, but we
	// surface a file-level parse error at line 1 so the editor
	// shows SOMETHING rather than a silent absence of lenses.
	path := describeFile(t, `not = valid = toml`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(desc.Diagnostics) != 1 {
		t.Fatalf("expected 1 file-level diagnostic, got %v", desc.Diagnostics)
	}
	if desc.Diagnostics[0].Rule != RuleConfigParseError {
		t.Errorf("rule: got %q want %q", desc.Diagnostics[0].Rule, RuleConfigParseError)
	}
	if len(desc.Projections) != 0 {
		t.Errorf("expected no projections on parse error, got %d", len(desc.Projections))
	}
}

func TestDescribe_PopulatesConnectionFromDefaultEnv(t *testing.T) {
	// Connection comes from the default env (the [env.*] block with
	// default = true). The LSP server surfaces it via
	// gaffer/projectionDetails so the editor can gate the "live" run
	// option in the picker.
	path := describeFile(t, `[env.local]
connection = "esdb://localhost:2113"
default = true

[[projection]]
name = "p"
entry = "p.js"
engine_version = 2
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if desc.Connection != "esdb://localhost:2113" {
		t.Errorf("Connection: got %q want %q", desc.Connection, "esdb://localhost:2113")
	}
}

func TestDescribe_OmitsConnectionWhenNoDefaultEnv(t *testing.T) {
	// An env without default = true yields no default connection;
	// Description.Connection stays empty so the editor gates the live
	// option.
	path := describeFile(t, `[env.prod]
connection = "esdb://prod:2113"

[[projection]]
name = "p"
entry = "p.js"
engine_version = 2
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if desc.Connection != "" {
		t.Errorf("Connection: got %q want empty (no default env)", desc.Connection)
	}
}

func TestDescribe_OmitsConnectionWhenAbsent(t *testing.T) {
	// Empty string (zero value) means "no connection declared" -
	// distinct from a declared empty string (which the strict
	// validator would catch). The editor reads nil here as
	// "fixture-only project; gate the live option".
	path := describeFile(t, `[[projection]]
name = "p"
entry = "p.js"
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if desc.Connection != "" {
		t.Errorf("Connection: got %q want empty", desc.Connection)
	}
}

func TestDescribe_NonexistentFileReturnsErrNotExist(t *testing.T) {
	// Pin the wrapped error so the LSP server can use
	// errors.Is(err, fs.ErrNotExist) to distinguish "file gone"
	// (drop URI's diagnostics) from other I/O failures.
	_, err := Describe(context.Background(), filepath.Join(t.TempDir(), "missing.toml"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("expected fs.ErrNotExist, got %v", err)
	}
}

func TestDescribe_RespectsContextCancellation(t *testing.T) {
	path := describeFile(t, `[[projection]]
name = "p"
entry = "p.js"
`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Describe(ctx, path)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestDescribe_ScannerParserDriftBecomesFileLevelDiagnostic(t *testing.T) {
	// Pathological case: a `[[projection]]` occurrence inside a
	// triple-quoted string is a scanner false positive (parser
	// ignores it). Counts disagree -> we return a file-level
	// diagnostic and skip projection rendering rather than zip
	// against mismatched arrays.
	path := describeFile(t, `value = """
[[projection]]
"""

[[projection]]
name = "real"
entry = "real.js"
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(desc.Diagnostics) == 0 {
		t.Fatalf("expected drift diagnostic")
	}
	if !strings.Contains(desc.Diagnostics[0].Message, "header lines") {
		t.Errorf("diagnostic message should mention drift: %q", desc.Diagnostics[0].Message)
	}
	if len(desc.Projections) != 0 {
		t.Errorf("expected no projections on drift, got %d", len(desc.Projections))
	}
}

func TestDescribe_ProjectionWithoutFixtures(t *testing.T) {
	// Zero-fixture happy path - common shape (most projections
	// won't have fixtures declared). Confirms no panic on a
	// projection.Fixtures map that's nil/empty.
	path := describeFile(t, `[[projection]]
name = "p"
entry = "p.js"
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(desc.Projections) != 1 {
		t.Fatalf("expected 1 projection, got %d", len(desc.Projections))
	}
	if len(desc.Projections[0].Fixtures) != 0 {
		t.Errorf("expected zero fixtures, got %v", desc.Projections[0].Fixtures)
	}
}

func TestDescribe_ConfigWithNoProjections(t *testing.T) {
	// Env-only config, no projections. Valid TOML, nothing to lens.
	// Empty projections + no diagnostics.
	path := describeFile(t, `[env.local]
connection = "kurrentdb://localhost:2113"
default = true
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(desc.Projections) != 0 {
		t.Errorf("expected no projections, got %v", desc.Projections)
	}
	if len(desc.Diagnostics) != 0 {
		t.Errorf("expected no diagnostics, got %v", desc.Diagnostics)
	}
}

func TestDescribe_DuplicateProjectionName(t *testing.T) {
	// Loose path catches duplicate names so the editor flags it
	// before the user clicks Run (strict Load would reject at
	// run-time). Diagnostic lands on the SECOND occurrence -
	// matches strict's "first wins" mental model.
	path := describeFile(t, `[[projection]]
name = "checkout"
entry = "a.js"

[[projection]]
name = "checkout"
entry = "b.js"
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(desc.Projections) != 2 {
		t.Fatalf("expected 2 projections, got %d", len(desc.Projections))
	}
	if desc.Projections[0].Diagnostic != nil {
		t.Errorf("first projection should be clean, got %+v", desc.Projections[0].Diagnostic)
	}
	d := desc.Projections[1].Diagnostic
	if d == nil || d.Rule != RuleProjectionDuplicateName {
		t.Fatalf("expected duplicate-name diagnostic on 2nd projection, got %+v", d)
	}
}

func TestDescribe_DuplicateNameDoesNotOverwriteExistingDiagnostic(t *testing.T) {
	// Second projection has a primary issue (missing entry) that
	// outranks the duplicate-name detection. First-issue-wins -
	// don't overwrite.
	path := describeFile(t, `[[projection]]
name = "p"
entry = "a.js"

[[projection]]
name = "p"
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	d := desc.Projections[1].Diagnostic
	if d == nil || d.Rule != RuleProjectionMissingEntry {
		t.Fatalf("expected missing-entry to win over duplicate-name, got %+v", d)
	}
}

func TestDescribe_ScanDriftRuleCodeIsDistinct(t *testing.T) {
	// Drift uses RuleConfigScanDrift, NOT RuleConfigParseError -
	// parser succeeded; the disagreement is between parser and
	// line scanner. Consumers may want to treat them differently.
	path := describeFile(t, `value = """
[[projection]]
"""

[[projection]]
name = "real"
entry = "real.js"
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if len(desc.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %v", desc.Diagnostics)
	}
	if desc.Diagnostics[0].Rule != RuleConfigScanDrift {
		t.Errorf("rule: got %q want %q", desc.Diagnostics[0].Rule, RuleConfigScanDrift)
	}
}

func TestDescribe_TrailingSlashFixturePathPasses(t *testing.T) {
	// `fixtures/dir/` survives validation - filepath.Clean strips
	// the trailing slash, no `..` prefix, isn't empty. Pin so a
	// future tightening (e.g. "must point at a file") doesn't
	// silently change the contract.
	path := describeFile(t, `[[projection]]
name = "p"
entry = "p.js"
fixtures.happy = "fixtures/dir/"
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	fx := desc.Projections[0].Fixtures[0]
	if fx.Diagnostic != nil {
		t.Fatalf("expected trailing-slash path valid, got %+v", fx)
	}
}

func TestDescribe_InternalDotDotResolvesInsideRoot(t *testing.T) {
	// `fixtures/sub/../happy.json` cleans to `fixtures/happy.json`
	// - inside root, must validate. Mirrors isWithin's behavior
	// in the extension and the CLI's Load validate() rule.
	path := describeFile(t, `[[projection]]
name = "p"
entry = "p.js"
fixtures.happy = "fixtures/sub/../happy.json"
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	fx := desc.Projections[0].Fixtures[0]
	if fx.Diagnostic != nil {
		t.Fatalf("expected internal `..` path valid, got %+v", fx)
	}
}

func TestDescribe_ProjectionRulePrecedence(t *testing.T) {
	// Multiple projection-level rules could fire; first-wins is
	// missing-name -> missing-entry -> entry-escapes. Pin the
	// order with a projection that triggers BOTH missing-entry
	// and entry-escapes (impossible for both to be set, so use
	// missing-name + entry-escapes by giving an entry but no name).
	path := describeFile(t, `[[projection]]
entry = "../escape.js"
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	d := desc.Projections[0].Diagnostic
	if d == nil || d.Rule != RuleProjectionMissingName {
		t.Fatalf("expected missing-name to win over entry-escapes, got %+v", d)
	}
}

func TestDescribe_FixtureRulePrecedence(t *testing.T) {
	// Fixture rules: empty-name -> empty-path -> escapes-root.
	// The empty-quoted-name case wins regardless of the path.
	path := describeFile(t, `[[projection]]
name = "p"
entry = "p.js"
fixtures."" = "../escape.json"
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	d := desc.Projections[0].Fixtures[0].Diagnostic
	if d == nil || d.Rule != RuleFixtureEmptyName {
		t.Fatalf("expected empty-name to win, got %+v", d)
	}
}

func TestDescribe_DriftRangeHasVisibleWidth(t *testing.T) {
	// rangeForLine(1, 0) produces an EndCol of 1, not 0, so the
	// diagnostic doesn't collapse to a zero-width caret. Pin the
	// behavior since it's only observable on file-level
	// diagnostics where the scanner has nothing to report a length
	// from.
	r := rangeForLine(1, 0)
	if r.EndCol != 1 {
		t.Errorf("expected EndCol=1 for length=0, got %d", r.EndCol)
	}
}

func TestDescribe_AbsPathsResolveRelativeToConfigDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	writeFile(t, path, `[[projection]]
name = "p"
entry = "src/p.js"
fixtures.happy = "fixtures/h.json"
`)
	desc, err := Describe(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	p := desc.Projections[0]
	wantEntry := filepath.Join(dir, "src", "p.js")
	if p.EntryAbsPath != wantEntry {
		t.Errorf("EntryAbsPath: got %q want %q", p.EntryAbsPath, wantEntry)
	}
	wantFixture := filepath.Join(dir, "fixtures", "h.json")
	if p.Fixtures[0].AbsPath != wantFixture {
		t.Errorf("Fixture AbsPath: got %q want %q", p.Fixtures[0].AbsPath, wantFixture)
	}
}

func TestDescribe_RelativeInputPathBecomesAbsolute(t *testing.T) {
	// Caller may pass a relative path - Description.ConfigFile is
	// always absolute so the LSP wire emits unambiguous paths.
	dir := t.TempDir()
	path := filepath.Join(dir, "gaffer.toml")
	writeFile(t, path, `[[projection]]
name = "p"
entry = "p.js"
`)

	t.Chdir(dir)

	desc, err := Describe(context.Background(), "./gaffer.toml")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(desc.ConfigFile) {
		t.Errorf("ConfigFile should be absolute, got %q", desc.ConfigFile)
	}
}
