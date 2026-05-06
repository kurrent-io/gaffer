package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/BurntSushi/toml"
)

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
//
// Use DescribeBytes when the content is already in memory (e.g.
// the LSP server's didChange flow).
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
	return DescribeBytes(ctx, absPath, data)
}

// DescribeBytes is Describe over in-memory content. The path is
// used to anchor ConfigFile and to resolve entry / fixture paths
// relative to its directory; no I/O is performed against the path
// itself. Used by the LSP server's didOpen / didChange flow where
// the in-memory buffer may differ from the on-disk file.
//
// As with Describe, returns an error only for context cancellation
// or path-resolution failures; parse errors and per-element issues
// flow back through Description.Diagnostics.
func DescribeBytes(ctx context.Context, path string, data []byte) (Description, error) {
	if err := ctx.Err(); err != nil {
		return Description{}, err
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return Description{}, err
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

	scan := scanLines(text)

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
	header projectionHeaderLine,
	scan scannedLines,
	tomlDir string,
	idx int,
) ProjectionDescription {
	headerRange := rangeForLine(header.Line, header.Length)
	entry := ProjectionDescription{
		Name:  p.Name,
		Entry: p.Entry,
		Range: headerRange,
	}
	if p.Entry != "" {
		entry.EntryAbsPath = filepath.Clean(filepath.Join(tomlDir, p.Entry))
	}

	// Shared with strict validate() via checkProjection - first
	// failing rule wins. Loose path attaches the rule code to the
	// diagnostic; strict path used the same message verbatim.
	if rule, msg, fail := checkProjection(p); fail {
		entry.Diagnostic = &Diagnostic{
			Range:   headerRange,
			Rule:    rule,
			Message: msg,
		}
	}

	// Find fixture lines belonging to this projection (between this
	// header and the next, or EOF). Scoped per-projection so two
	// projections each declaring `fixtures.happy` don't collide.
	nextLine := -1
	if idx+1 < len(scan.ProjectionHeaders) {
		nextLine = scan.ProjectionHeaders[idx+1].Line
	}
	fixtureLines := map[string]fixtureKeyLine{}
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
			p.Name, name, p.Fixtures[name], fixtureLines, headerRange, tomlDir,
		))
	}
	return entry
}

func describeFixture(
	projection, name, path string,
	fixtureLines map[string]fixtureKeyLine,
	fallbackRange SourceRange,
	tomlDir string,
) FixtureDescription {
	// Range: prefer the fixture's `fixtures.<name>` line when the
	// dotted-key form was used; fall back to the projection header
	// for inline-table or [projection.fixtures] table forms where
	// no per-key source line exists.
	r := fallbackRange
	if fl, ok := fixtureLines[name]; ok {
		r = rangeForLine(fl.Line, fl.Length)
	}

	fx := FixtureDescription{
		Name:  name,
		Path:  path,
		Range: r,
	}

	// Shared with strict validate() via checkFixture - same rule
	// list, same ordering. Strict's projection name lives outside
	// the fixture; loose passes it through for message formatting.
	if rule, msg, fail := checkFixture(projection, name, path); fail {
		fx.Diagnostic = &Diagnostic{
			Range:   r,
			Rule:    rule,
			Message: msg,
		}
		return fx
	}
	fx.AbsPath = filepath.Clean(filepath.Join(tomlDir, path))
	return fx
}
