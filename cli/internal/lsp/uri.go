package lsp

import (
	"path"
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
// unchanged if it doesn't look like a file URI or if parsing
// panics (the library panics on some malformed URIs).
//
// Forward-slash convention applies at URI / comparison
// boundaries: every consumer of this output - map keys for
// dispatch, `samePath`, `os.ReadFile` (which accepts forward
// slashes on Windows) - works with forward slashes on every
// OS. Disk-side calls into `internal/config` keep native
// separators since `filepath` handles them; the boundary is
// here.
func uriToPath(s string) (out string) {
	if !strings.HasPrefix(s, "file://") {
		return s
	}
	out = s
	defer func() {
		if r := recover(); r != nil {
			out = s
		}
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
	a = path.Clean(strings.ReplaceAll(a, `\`, `/`))
	b = path.Clean(strings.ReplaceAll(b, `\`, `/`))
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
