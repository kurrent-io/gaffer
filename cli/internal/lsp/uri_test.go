package lsp

import (
	"strings"
	"testing"
)

func TestPathToURI_RoundTrip(t *testing.T) {
	// pathToURI -> uriToPath must be the identity for every path
	// the editor could plausibly hand us. Bug 1 was a divergent
	// encoding (spaces, unicode, special chars) that broke
	// document-store map-key lookups.
	// Note: paths containing `?`, `#`, or literal `%XX`
	// sequences are technically valid on POSIX but break
	// round-trip via go.lsp.dev/uri (treated as URL query /
	// fragment delimiters or aggressively percent-decoded).
	// Acceptable - those chars in real workspace paths are
	// vanishingly rare.
	cases := []string{
		"/home/george/dev/gaffer/gaffer.toml",
		"/home/george/foo bar/gaffer.toml",
		"/home/george/résumé/gaffer.toml",
		"/home/george/with:colon/gaffer.toml",
		"/home/george/мир/gaffer.toml",
	}
	for _, p := range cases {
		uri := pathToURI(p)
		if !strings.HasPrefix(uri, "file://") {
			t.Errorf("pathToURI(%q) = %q, missing file:// prefix", p, uri)
			continue
		}
		got := uriToPath(uri)
		if got != p {
			t.Errorf("round-trip mismatch:\n  in:  %q\n  uri: %q\n  out: %q", p, uri, got)
		}
	}
}

func TestPathToURI_EscapesSpaces(t *testing.T) {
	// Pin the canonical encoding: spaces become %20, matching
	// what LSP clients (vscode-languageclient et al) produce.
	got := pathToURI("/foo bar/gaffer.toml")
	want := "file:///foo%20bar/gaffer.toml"
	if got != want {
		t.Errorf("pathToURI: got %q want %q", got, want)
	}
}

func TestPathToURI_WindowsDriveLetter(t *testing.T) {
	// go.lsp.dev/uri auto-detects drive-letter paths and emits
	// the LSP-canonical `file:///C:/foo/x.toml` shape regardless
	// of which OS we run on. Pin so a future library swap
	// doesn't silently regress Windows clients.
	got := pathToURI("C:\\foo\\gaffer.toml")
	want := "file:///C:/foo/gaffer.toml"
	if got != want {
		t.Errorf("pathToURI(windows path): got %q want %q", got, want)
	}
}

func TestUriToPath_WindowsDriveLetterReturnsForwardSlash(t *testing.T) {
	// LSP layer keeps paths in forward-slash form internally so
	// downstream consumers (`os.ReadFile`, map keys, samePath)
	// don't have to deal with separator differences. Confirm the
	// drive-letter path comes out without the leading slash and
	// without backslashes.
	got := uriToPath("file:///C:/foo/gaffer.toml")
	want := "C:/foo/gaffer.toml"
	if got != want {
		t.Errorf("uriToPath(windows uri): got %q want %q", got, want)
	}
}

func TestSamePath_Equality(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"/foo/bar.js", "/foo/bar.js", true},
		{"/foo/bar.js", "/foo/baz.js", false},
		// slash-fold: different separators, same file
		{"C:/foo/bar.js", "C:\\foo\\bar.js", true},
	}
	for _, c := range cases {
		if got := samePath(c.a, c.b); got != c.want {
			t.Errorf("samePath(%q, %q): got %v want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestUriToPath_NonFileURIPassesThrough(t *testing.T) {
	// Tests sometimes pass raw paths or other URI shapes through
	// uriToPath; the helper preserves them rather than mangling
	// inputs it can't recognise.
	cases := []struct{ in, want string }{
		{"/plain/path", "/plain/path"},
		{"http://example.com/x", "http://example.com/x"},
		{"", ""},
	}
	for _, c := range cases {
		if got := uriToPath(c.in); got != c.want {
			t.Errorf("uriToPath(%q): got %q want %q", c.in, got, c.want)
		}
	}
}

func TestIsGafferConfig_BasenameGate(t *testing.T) {
	// Pin the basename check that defends against false positives
	// like notgaffer.toml. Comment in parse.go calls this out as
	// a defensive choice; this is the test that documents it.
	cases := []struct {
		uri  string
		want bool
	}{
		{"file:///gaffer.toml", true},
		{"file:///workspace/gaffer.toml", true},
		{"file:///workspace/sub/gaffer.toml", true},
		{"file:///workspace/notgaffer.toml", false},
		{"file:///workspace/mygaffer.toml", false},
		{"file:///workspace/gaffer.toml.bak", false},
		{"file:///workspace/gaffer.tomlx", false},
		{"file:///workspace/projection.js", false},
	}
	for _, c := range cases {
		if got := isGafferConfig(c.uri); got != c.want {
			t.Errorf("isGafferConfig(%q): got %v want %v", c.uri, got, c.want)
		}
	}
}
