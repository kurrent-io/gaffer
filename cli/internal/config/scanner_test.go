package config

import (
	"reflect"
	"testing"
)

func TestScanLines_LocatesProjectionHeaders(t *testing.T) {
	text := "[[projection]]\nname = \"a\"\n\n[[projection]]\n"
	got := scanLines(text)
	want := []projectionHeaderLine{
		{Line: 1, Length: len("[[projection]]")},
		{Line: 4, Length: len("[[projection]]")},
	}
	if !reflect.DeepEqual(got.ProjectionHeaders, want) {
		t.Fatalf("ProjectionHeaders mismatch:\n got %+v\nwant %+v", got.ProjectionHeaders, want)
	}
}

func TestScanLines_HeaderWithWhitespaceAndComment(t *testing.T) {
	// Leading spaces + trailing comment + interior spaces inside
	// the brackets all allowed (BurntSushi/toml accepts them too).
	text := "  [[ projection ]] # main"
	got := scanLines(text)
	if len(got.ProjectionHeaders) != 1 {
		t.Fatalf("expected 1 header, got %d", len(got.ProjectionHeaders))
	}
	if got.ProjectionHeaders[0].Line != 1 {
		t.Fatalf("expected line 1, got %d", got.ProjectionHeaders[0].Line)
	}
}

func TestScanLines_LocatesFixturesAndCapturesNames(t *testing.T) {
	text := "[[projection]]\nfixtures.happy = \"a.json\"\nfixtures.edge = \"b.json\"\n"
	got := scanLines(text)
	if len(got.FixtureLines) != 2 {
		t.Fatalf("expected 2 fixture lines, got %d", len(got.FixtureLines))
	}
	if got.FixtureLines[0].Line != 2 || got.FixtureLines[0].Name != "happy" {
		t.Errorf("fixture[0] mismatch: %+v", got.FixtureLines[0])
	}
	if got.FixtureLines[1].Line != 3 || got.FixtureLines[1].Name != "edge" {
		t.Errorf("fixture[1] mismatch: %+v", got.FixtureLines[1])
	}
}

func TestScanLines_LocatesEnvHeaders(t *testing.T) {
	text := "[env.local]\nconnection = \"x\"\n\n[env.prod]\n"
	got := scanLines(text)
	want := []envHeaderLine{
		{Line: 1, Length: len("[env.local]"), Name: "local"},
		{Line: 4, Length: len("[env.prod]"), Name: "prod"},
	}
	if !reflect.DeepEqual(got.EnvHeaders, want) {
		t.Fatalf("EnvHeaders mismatch:\n got %+v\nwant %+v", got.EnvHeaders, want)
	}
}

func TestScanLines_EnvHeaderWhitespaceAndComment(t *testing.T) {
	// Leading spaces, interior spaces around brackets/dot, and a
	// trailing comment are all legal TOML the scanner must tolerate.
	text := "  [ env . staging ]  # target"
	got := scanLines(text)
	if len(got.EnvHeaders) != 1 || got.EnvHeaders[0].Name != "staging" {
		t.Fatalf("expected 1 env header named staging, got %+v", got.EnvHeaders)
	}
}

func TestScanLines_EnvHeaderIgnoresSubTablesAndQuotedKeys(t *testing.T) {
	// `[env.<name>.oauth]` sub-tables and quoted keys (`[env."x"]`)
	// carry no top-level env range; they fall through, like quoted
	// fixture keys.
	text := "[env.prod.oauth]\n[env.\"weird name\"]\n"
	got := scanLines(text)
	if len(got.EnvHeaders) != 0 {
		t.Fatalf("expected 0 env headers, got %+v", got.EnvHeaders)
	}
}

func TestScanLines_ToleratesWhitespaceAroundDot(t *testing.T) {
	// TOML grammar permits whitespace around dotted keys. Without
	// allowing it, the scanner would miss legal source.
	text := "  fixtures . happy   = \"a.json\""
	got := scanLines(text)
	if len(got.FixtureLines) != 1 {
		t.Fatalf("expected 1 fixture line, got %d", len(got.FixtureLines))
	}
	if got.FixtureLines[0].Name != "happy" {
		t.Fatalf("expected name happy, got %q", got.FixtureLines[0].Name)
	}
}

func TestScanLines_DoesNotMatchInlineTableForm(t *testing.T) {
	// `fixtures = { happy = "a", edge = "b" }` has no per-key line;
	// the scanner correctly returns nothing. Callers fall back to
	// the projection-header range for these fixtures.
	text := "[[projection]]\nfixtures = { happy = \"a.json\" }"
	got := scanLines(text)
	if len(got.FixtureLines) != 0 {
		t.Fatalf("expected 0 fixture lines, got %+v", got.FixtureLines)
	}
}

func TestScanLines_DoesNotMatchPrefixedNames(t *testing.T) {
	// `fixturesNot.happy` shares a prefix but isn't `fixtures.<key>`.
	// Anchored regex must reject.
	text := "fixturesNot.happy = \"a.json\""
	got := scanLines(text)
	if len(got.FixtureLines) != 0 {
		t.Fatalf("expected 0 fixture lines, got %+v", got.FixtureLines)
	}
}

func TestScanLines_DoesNotMatchQuotedKeys(t *testing.T) {
	// `fixtures."weird name" = "..."` parses fine in BurntSushi
	// (and smol-toml) but the scanner doesn't capture quoted keys -
	// per V0 design, no per-line lens for those, fall through to
	// projection-level dropdown.
	text := "fixtures.\"weird name\" = \"a.json\""
	got := scanLines(text)
	if len(got.FixtureLines) != 0 {
		t.Fatalf("expected 0 fixture lines, got %+v", got.FixtureLines)
	}
}

func TestScanLines_DoesNotMatchCommentLines(t *testing.T) {
	// Anchored regex must not match commented-out fixture lines.
	text := "# fixtures.happy = \"a.json\"\n   # fixtures.edge = \"b.json\""
	got := scanLines(text)
	if len(got.FixtureLines) != 0 {
		t.Fatalf("expected 0 fixture lines, got %+v", got.FixtureLines)
	}
}

func TestScanLines_NormalisesWindowsLineEndings(t *testing.T) {
	// CRLF source must produce the same line numbers as LF.
	text := "[[projection]]\r\nfixtures.happy = \"a.json\"\r\n"
	got := scanLines(text)
	if len(got.ProjectionHeaders) != 1 || got.ProjectionHeaders[0].Line != 1 {
		t.Errorf("projection header: %+v", got.ProjectionHeaders)
	}
	if len(got.FixtureLines) != 1 || got.FixtureLines[0].Line != 2 {
		t.Errorf("fixture line: %+v", got.FixtureLines)
	}
}

func TestScanLines_EmptyInput(t *testing.T) {
	got := scanLines("")
	if got.ProjectionHeaders != nil || got.FixtureLines != nil {
		t.Fatalf("expected zero-value scannedLines, got %+v", got)
	}
}

func TestScanLines_StripsLeadingUTF8BOM(t *testing.T) {
	// Some Windows editors emit a UTF-8 BOM at file start. Without
	// stripping, the ^\s* anchor doesn't consume the BOM and the
	// header on line 1 would be missed. Pin the strip.
	text := "\uFEFF[[projection]]\nfixtures.happy = \"a.json\""
	got := scanLines(text)
	if len(got.ProjectionHeaders) != 1 || got.ProjectionHeaders[0].Line != 1 {
		t.Errorf("projection header missed under BOM: %+v", got.ProjectionHeaders)
	}
	if len(got.FixtureLines) != 1 || got.FixtureLines[0].Line != 2 {
		t.Errorf("fixture line wrong under BOM: %+v", got.FixtureLines)
	}
}

func TestScanLines_TrailingWhitespaceOnHeader(t *testing.T) {
	// `[[projection]]   ` should still match — the \s* tail eats it.
	text := "[[projection]]   "
	got := scanLines(text)
	if len(got.ProjectionHeaders) != 1 {
		t.Fatalf("expected 1 header, got %+v", got.ProjectionHeaders)
	}
}

func TestScanLines_FixtureNameWithLeadingDigit(t *testing.T) {
	// TOML bare keys allow leading digits. The regex's [A-Za-z0-9_-]+
	// matches; pin it so a future tightening doesn't silently break
	// numeric-prefixed fixture names.
	text := "fixtures.1happy = \"a.json\""
	got := scanLines(text)
	if len(got.FixtureLines) != 1 || got.FixtureLines[0].Name != "1happy" {
		t.Fatalf("expected name 1happy, got %+v", got.FixtureLines)
	}
}

func TestScanLines_HeaderInsideMultilineStringIsAFalsePositive(t *testing.T) {
	// Known limitation of the line-scan approach: a `[[projection]]`
	// occurrence inside a TOML triple-quoted string is treated as a
	// header. The TOML parser correctly ignores it, so the scanner
	// and parser disagree on count. Lock the false-positive in so a
	// future reader doesn't think it's a bug; if we ever care, the
	// fix is real TOML tokenisation, not a regex tweak.
	text := "value = \"\"\"\n[[projection]]\n\"\"\""
	got := scanLines(text)
	if len(got.ProjectionHeaders) != 1 {
		t.Fatalf("expected 1 false-positive header (limitation), got %+v", got.ProjectionHeaders)
	}
}

func TestScanLines_LinesAre1Indexed(t *testing.T) {
	// Pin the convention - LSP wire is 1-indexed and the scanner
	// matches. A 0-indexed regression here would break every
	// editor's lens placement until conversion code was added.
	got := scanLines("[[projection]]")
	if len(got.ProjectionHeaders) != 1 {
		t.Fatalf("expected 1 header")
	}
	if got.ProjectionHeaders[0].Line != 1 {
		t.Fatalf("expected line 1 (1-indexed), got %d", got.ProjectionHeaders[0].Line)
	}
}
