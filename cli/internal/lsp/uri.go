package lsp

import (
	"net/url"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

// pathToURI converts an absolute filesystem path to a file:// URI.
// Uses url.URL.String so the encoding matches what LSP clients
// produce - e.g. spaces become `%20`, `:` in path segments stays
// `:`. Hand-concatenating "file://" + EscapedPath would produce a
// non-canonical form that diverges from the client's URI string,
// breaking map-key lookups in the document store.
//
// V1 is Linux-only at the editor / LSP layer; Windows would need
// `file:///C:/...` shaping but no editor extension on Windows is
// in scope yet.
func pathToURI(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

// uriToPath strips the file:// scheme and returns the absolute
// filesystem path. Returns the input unchanged if it doesn't look
// like a file URI - lets callers pass through raw paths during
// tests without needing a separate code path.
func uriToPath(uri string) string {
	if !strings.HasPrefix(uri, "file://") {
		return uri
	}
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	return u.Path
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
