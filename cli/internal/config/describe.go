package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
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
	ConfigFile  string            `json:"configFile"`
	Format      string            `json:"format"`
	Projections []ProjectionEntry `json:"projections,omitempty"`
	Diagnostics []Diagnostic      `json:"diagnostics,omitempty"`
}

// ProjectionEntry is a single projection's view: name, entry path
// (raw + resolved), source range of its [[projection]] header,
// per-fixture details, plus an optional projection-level diagnostic
// (missing name, escaping entry path, duplicate name).
type ProjectionEntry struct {
	Name         string         `json:"name"`
	Entry        string         `json:"entry"`
	EntryAbsPath string         `json:"entryAbsPath,omitempty"`
	Range        SourceRange    `json:"range"`
	Fixtures     []FixtureEntry `json:"fixtures,omitempty"`
	Diagnostic   *Diagnostic    `json:"diagnostic,omitempty"`
}

// FixtureEntry is a single fixture's view: name, path (raw +
// resolved), source range (the `fixtures.<name>` line if the
// dotted-key form was used, else the projection header range),
// validity, and a diagnostic when invalid.
type FixtureEntry struct {
	Name       string      `json:"name"`
	Path       string      `json:"path"`
	AbsPath    string      `json:"absPath,omitempty"`
	Range      SourceRange `json:"range"`
	Valid      bool        `json:"valid"`
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

// rangeForLine builds a single-line SourceRange covering an entire
// line as reported by the scanner. Length=0 produces a 1-char wide
// range so a diagnostic on an effectively-empty line still has a
// visible target instead of collapsing to a caret.
func rangeForLine(line, length int) SourceRange {
	end := length
	if end == 0 {
		end = 1
	}
	return SourceRange{
		StartLine: line,
		StartCol:  0,
		EndLine:   line,
		EndCol:    end,
	}
}

// Describe reads `path`, parses + validates loosely, and returns a
// Description suitable for emitting LSP code lenses and
// diagnostics. Returns an error only for unrecoverable I/O failures
// (file unreadable, etc.); parse errors and per-element issues
// flow back through Description.Diagnostics.
//
// `ctx` cancels promptly between major steps. The work itself is
// sub-ms per file so cancellation is cheap insurance, not a
// frequently-hit code path.
func Describe(ctx context.Context, path string) (Description, error) {
	if err := ctx.Err(); err != nil {
		return Description{}, err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return Description{}, err
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		// Wrap so callers can use errors.Is(err, fs.ErrNotExist) to
		// distinguish "file gone" (drop diagnostics for that URI)
		// from "permission denied" or other I/O failures (surface
		// to user).
		return Description{}, fmt.Errorf("read config: %w", err)
	}

	desc := Description{
		ConfigFile: absPath,
		// Format is on the wire so consumers can switch on it once a
		// second config format is supported. Always "toml" today.
		Format: "toml",
	}
	tomlDir := filepath.Dir(absPath)
	text := string(data)

	if err := ctx.Err(); err != nil {
		return Description{}, err
	}

	// Parse first; on failure, surface as a file-level diagnostic
	// pinned to line 1 (we don't have a real position from
	// BurntSushi without parsing the error message). Editor still
	// gets a squiggle they can see.
	var cfg Config
	if _, err := toml.Decode(text, &cfg); err != nil {
		desc.Diagnostics = append(desc.Diagnostics, Diagnostic{
			Range:   rangeForLine(1, 0),
			Rule:    RuleConfigParseError,
			Message: err.Error(),
		})
		return desc, nil
	}

	if err := ctx.Err(); err != nil {
		return Description{}, err
	}

	scan := ScanLines(text)

	// Drift: BurntSushi found N projections, the line scanner found
	// M headers, and they disagree. Source is malformed in a way
	// neither half can recover from cleanly. Skip projection
	// rendering and surface a file-level diagnostic - matches the
	// TS extension's existing "skip the file" path.
	if len(cfg.Projection) != len(scan.ProjectionHeaders) {
		desc.Diagnostics = append(desc.Diagnostics, Diagnostic{
			Range: rangeForLine(1, 0),
			Rule:  RuleConfigScanDrift,
			Message: fmt.Sprintf(
				"%d [[projection]] blocks parsed but %d header lines found",
				len(cfg.Projection),
				len(scan.ProjectionHeaders),
			),
		})
		return desc, nil
	}

	for i, p := range cfg.Projection {
		header := scan.ProjectionHeaders[i]
		entry := describeProjection(p, header, scan, tomlDir, i)
		desc.Projections = append(desc.Projections, entry)
	}
	// Duplicate-name detection runs after the per-projection pass.
	// Strict Load rejects the second occurrence; loose Describe
	// flags it on the duplicate's projection diagnostic so the
	// editor highlights what the user has to change. Doesn't
	// overwrite an existing diagnostic - first-issue-wins.
	seen := map[string]bool{}
	for i := range desc.Projections {
		name := desc.Projections[i].Name
		if name == "" {
			continue
		}
		if seen[name] {
			if desc.Projections[i].Diagnostic == nil {
				r := desc.Projections[i].Range
				desc.Projections[i].Diagnostic = &Diagnostic{
					Range:   r,
					Rule:    RuleProjectionDuplicateName,
					Message: fmt.Sprintf("duplicate projection name: %q", name),
				}
			}
			continue
		}
		seen[name] = true
	}
	return desc, nil
}

func describeProjection(
	p Projection,
	header ProjectionHeaderLine,
	scan ScannedLines,
	tomlDir string,
	idx int,
) ProjectionEntry {
	headerRange := rangeForLine(header.Line, header.Length)
	entry := ProjectionEntry{
		Name:  p.Name,
		Entry: p.Entry,
		Range: headerRange,
	}
	if p.Entry != "" {
		entry.EntryAbsPath = filepath.Clean(filepath.Join(tomlDir, p.Entry))
	}

	// First projection-level issue wins. The LSP server can only
	// publish one diagnostic per projection-header line cleanly
	// anyway; ordering matches the strictest sequential validation.
	switch {
	case p.Name == "":
		entry.Diagnostic = &Diagnostic{
			Range:   headerRange,
			Rule:    RuleProjectionMissingName,
			Message: "projection missing required field: name",
		}
	case p.Entry == "":
		entry.Diagnostic = &Diagnostic{
			Range:   headerRange,
			Rule:    RuleProjectionMissingEntry,
			Message: fmt.Sprintf("projection %q missing required field: entry", p.Name),
		}
	case strings.HasPrefix(filepath.Clean(p.Entry), ".."):
		entry.Diagnostic = &Diagnostic{
			Range: headerRange,
			Rule:  RuleProjectionEntryEscapesRoot,
			Message: fmt.Sprintf(
				"projection %q entry must not escape project root: %s",
				p.Name, p.Entry,
			),
		}
	}

	// Find fixture lines belonging to this projection (between this
	// header and the next, or EOF). Scoped per-projection so two
	// projections each declaring `fixtures.happy` don't collide.
	nextLine := -1
	if idx+1 < len(scan.ProjectionHeaders) {
		nextLine = scan.ProjectionHeaders[idx+1].Line
	}
	fixtureLines := map[string]FixtureKeyLine{}
	for _, fl := range scan.FixtureLines {
		if fl.Line <= header.Line {
			continue
		}
		if nextLine != -1 && fl.Line >= nextLine {
			continue
		}
		fixtureLines[fl.Name] = fl
	}

	// Iterate fixtures in alphabetical order to match
	// FixtureNames()'s contract. Stable across runs regardless of
	// Go map iteration randomness.
	names := make([]string, 0, len(p.Fixtures))
	for name := range p.Fixtures {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		entry.Fixtures = append(entry.Fixtures, describeFixture(
			name, p.Fixtures[name], fixtureLines, headerRange, tomlDir,
		))
	}
	return entry
}

func describeFixture(
	name, path string,
	fixtureLines map[string]FixtureKeyLine,
	fallbackRange SourceRange,
	tomlDir string,
) FixtureEntry {
	// Range: prefer the fixture's `fixtures.<name>` line when the
	// dotted-key form was used; fall back to the projection header
	// for inline-table or [projection.fixtures] table forms where
	// no per-key source line exists.
	r := fallbackRange
	if fl, ok := fixtureLines[name]; ok {
		r = rangeForLine(fl.Line, fl.Length)
	}

	fx := FixtureEntry{
		Name:  name,
		Path:  path,
		Range: r,
	}

	switch {
	case name == "":
		fx.Diagnostic = &Diagnostic{
			Range:   r,
			Rule:    RuleFixtureEmptyName,
			Message: "fixture has an empty name",
		}
	case path == "":
		fx.Diagnostic = &Diagnostic{
			Range:   r,
			Rule:    RuleFixtureEmptyPath,
			Message: fmt.Sprintf("fixture %q has empty path", name),
		}
	case strings.HasPrefix(filepath.Clean(path), ".."):
		fx.Diagnostic = &Diagnostic{
			Range: r,
			Rule:  RuleFixturePathEscapesRoot,
			Message: fmt.Sprintf(
				"fixture %q path must not escape project root: %s",
				name, path,
			),
		}
	default:
		fx.Valid = true
		fx.AbsPath = filepath.Clean(filepath.Join(tomlDir, path))
	}
	return fx
}
