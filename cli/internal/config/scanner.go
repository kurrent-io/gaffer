package config

import (
	"regexp"
	"strings"
)

// projectionHeaderLine locates a `[[projection]]` array-of-tables
// header in source order. Line is 1-indexed; Length is the byte
// length of the line.
type projectionHeaderLine struct {
	Line   int
	Length int
}

// fixtureKeyLine locates a `fixtures.<name> = ...` line in source
// order. Name is the captured key (TOML bare-key syntax only).
type fixtureKeyLine struct {
	Line   int
	Length int
	Name   string
}

// envHeaderLine locates an `[env.<name>]` table header in source
// order. Name is the captured env key (TOML bare-key syntax only;
// quoted keys are not captured, matching the fixture-line rule).
type envHeaderLine struct {
	Line   int
	Length int
	Name   string
}

// scannedLines is the source-position view of a config file: every
// projection header line, every per-fixture key line, and every
// [env.<name>] header line. The TOML parser returns values but not
// positions; this scan recovers them so Describe can anchor lenses
// and diagnostics on real source lines.
//
// Lines are 1-indexed for direct use on the LSP wire. Length is the
// byte length of the line - safe to use as a column-end position
// for ASCII content. Non-ASCII content (e.g. unicode in a comment)
// would over-report the column on the LSP wire (which uses UTF-16
// code units); clients clamp, so the worst case is a slightly-
// extended highlight rather than misalignment.
//
// All scanner types are unexported - the only consumer is Describe
// inside this package. Promote back if a future caller wants raw
// positions.
type scannedLines struct {
	ProjectionHeaders []projectionHeaderLine
	FixtureLines      []fixtureKeyLine
	EnvHeaders        []envHeaderLine
}

// Bare `[[projection]]` header. Allow leading whitespace, interior
// spaces, trailing line comment.
var projectionHeaderPattern = regexp.MustCompile(`^\s*\[\[\s*projection\s*\]\]\s*(?:#.*)?$`)

// `fixtures.<name> = ...` where <name> is a bare TOML key
// ([A-Za-z0-9_-]+). Quoted keys are not matched; the dropdown still
// works for them via the parser, but no per-line lens.
var fixtureKeyPattern = regexp.MustCompile(`^\s*fixtures\s*\.\s*([A-Za-z0-9_-]+)\s*=`)

// `[env.<name>]` table header where <name> is a bare TOML key. The
// anchored `\]` after the name rejects sub-tables like
// `[env.<name>.oauth]`, and the bare-key class rejects quoted keys
// (`[env."../x"]`) - both fall through to no source range, like
// quoted fixture keys.
var envHeaderPattern = regexp.MustCompile(`^\s*\[\s*env\s*\.\s*([A-Za-z0-9_-]+)\s*\]\s*(?:#.*)?$`)

// scanLines walks `text` line-by-line and returns the position info
// for every projection header and fixture-key line, in source order.
func scanLines(text string) scannedLines {
	var out scannedLines
	for i, line := range splitLines(text) {
		if projectionHeaderPattern.MatchString(line) {
			out.ProjectionHeaders = append(out.ProjectionHeaders, projectionHeaderLine{
				Line:   i + 1,
				Length: len(line),
			})
			continue
		}
		if m := fixtureKeyPattern.FindStringSubmatch(line); m != nil {
			out.FixtureLines = append(out.FixtureLines, fixtureKeyLine{
				Line:   i + 1,
				Length: len(line),
				Name:   m[1],
			})
			continue
		}
		if m := envHeaderPattern.FindStringSubmatch(line); m != nil {
			out.EnvHeaders = append(out.EnvHeaders, envHeaderLine{
				Line:   i + 1,
				Length: len(line),
				Name:   m[1],
			})
		}
	}
	return out
}

// Strip a leading UTF-8 BOM if present (Windows editors sometimes
// add one), then normalise \r\n to \n and split on \n. A trailing
// newline produces an empty final element that doesn't match either
// pattern, harmless.
func splitLines(text string) []string {
	text = strings.TrimPrefix(text, "\uFEFF")
	return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
}
