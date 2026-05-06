package lsp

import (
	"path/filepath"
	"strings"

	"go.lsp.dev/uri"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

// pathToURI converts a filesystem path to a `file://` URI. Uses
// go.lsp.dev/uri so Windows drive letters and special characters
// are encoded the way LSP clients expect (e.g.
// `file:///C:/foo/gaffer.toml`, with the drive letter in the path
// portion and forward slashes throughout).
//
// We pre-normalise `\` -> `/` so the library's drive-letter
// detection works for Windows-shape input regardless of which
// OS the binary is running on; the library only auto-handles
// backslashes when GOOS=windows. Linux filenames with literal
// backslashes (vanishingly rare) get coerced; acceptable for V0.
func pathToURI(path string) string {
	return string(uri.File(strings.ReplaceAll(path, `\`, `/`)))
}

// uriToPath strips the file:// scheme and returns an absolute
// filesystem path in forward-slash form. Returns the input
// unchanged if it doesn't look like a file URI.
//
// Forward-slash internal: every consumer of this output - map
// keys, `os.ReadFile`, path comparison helpers - works with
// forward slashes on every OS we target. We pay one
// `filepath.ToSlash` here so the rest of the codebase doesn't
// have to think about separator differences.
func uriToPath(s string) string {
	if !strings.HasPrefix(s, "file://") {
		return s
	}
	defer func() {
		// uri.URI.Filename panics on malformed input. Convert any
		// panic into a fall-through return of the raw string so a
		// bad URI in production logs a "couldn't parse" later
		// rather than crashing the handler goroutine.
		_ = recover()
	}()
	return filepath.ToSlash(uri.New(s).Filename())
}

// samePath reports whether two paths refer to the same file
// according to the host OS's case-folding rules. Both paths are
// slash-normalised first so a Windows/macOS comparison of
// `C:\foo\X` vs `c:/foo/x` succeeds.
//
// Strings.EqualFold does Unicode case folding which is correct
// for ASCII (the common case) and reasonable for most filenames;
// it doesn't handle Turkish-locale dotted-i edge cases or NFC/NFD
// normalisation differences on macOS. Acceptable for V0; if a
// user reports a missed lens on a unicode path we revisit.
func samePath(a, b string) bool {
	a = strings.ReplaceAll(a, `\`, `/`)
	b = strings.ReplaceAll(b, `\`, `/`)
	if pathsCaseFold {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// rangeToLSP converts the config package's 1-indexed-line / 0-
// indexed-col SourceRange into the LSP wire format (0-indexed
// throughout). Negative inputs are clamped to 0; the upstream
// scanner doesn't produce them but defending against an unexpected
// regression is cheap.
func rangeToLSP(r config.SourceRange) Range {
	return Range{
		Start: Position{
			Line:      max0(r.StartLine - 1),
			Character: max0(r.StartCol),
		},
		End: Position{
			Line:      max0(r.EndLine - 1),
			Character: max0(r.EndCol),
		},
	}
}

func max0(x int) int {
	if x < 0 {
		return 0
	}
	return x
}
