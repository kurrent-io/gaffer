package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

func TestScaffold(t *testing.T) {
	p := testutil.NewProject(t).AddProjection("existing", "// placeholder").Save()

	result, err := Scaffold(p.Dir, p.Cfg, "counter", "projections/counter.js", "category:order", "per-stream", true)
	if err != nil {
		t.Fatal(err)
	}

	if result.Name != "counter" {
		t.Errorf("name: got %q, want %q", result.Name, "counter")
	}
	if result.RelPath != "projections/counter.js" {
		t.Errorf("relPath: got %q, want %q", result.RelPath, "projections/counter.js")
	}

	content, err := os.ReadFile(filepath.Join(p.Dir, "projections/counter.js"))
	if err != nil {
		t.Fatal(err)
	}
	testutil.AssertGolden(t, "scaffold_category_perstream_emit", string(content))

	reloaded, err := config.Load(filepath.Join(p.Dir, "gaffer.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.FindProjection("counter") == nil {
		t.Error("expected counter in saved config")
	}
	if reloaded.FindProjection("existing") == nil {
		t.Error("expected existing projection preserved")
	}
}

func TestScaffold_CustomPath(t *testing.T) {
	p := testutil.NewProject(t).Save()

	result, err := Scaffold(p.Dir, p.Cfg, "totals", "lib/handlers/totals.js", "all", "none", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.RelPath != "lib/handlers/totals.js" {
		t.Errorf("relPath: got %q, want %q", result.RelPath, "lib/handlers/totals.js")
	}
	if _, err := os.Stat(filepath.Join(p.Dir, "lib/handlers/totals.js")); err != nil {
		t.Errorf("expected file at lib/handlers/totals.js: %v", err)
	}
}

func TestScaffold_NameDistinctFromPath(t *testing.T) {
	// The toml key (name) and the file basename can differ - the
	// CLI's --name flag plumbs through to here.
	p := testutil.NewProject(t).Save()

	result, err := Scaffold(p.Dir, p.Cfg, "order-totals", "projections/totals.js", "all", "none", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "order-totals" {
		t.Errorf("name: got %q, want %q", result.Name, "order-totals")
	}

	reloaded, err := config.Load(filepath.Join(p.Dir, "gaffer.toml"))
	if err != nil {
		t.Fatal(err)
	}
	proj := reloaded.FindProjection("order-totals")
	if proj == nil {
		t.Fatal("expected order-totals in saved config")
	}
	if proj.Entry != "projections/totals.js" {
		t.Errorf("entry: got %q, want %q", proj.Entry, "projections/totals.js")
	}
}

func TestScaffold_DuplicateName(t *testing.T) {
	p := testutil.NewProject(t).AddProjection("existing", "// placeholder").Save()

	_, err := Scaffold(p.Dir, p.Cfg, "existing", "projections/existing.js", "all", "none", false)
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestScaffold_FileAlreadyExists(t *testing.T) {
	p := testutil.NewProject(t).AddProjection("existing", "// placeholder").Save()

	projDir := filepath.Join(p.Dir, "projections")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "taken.js"), []byte("//"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Scaffold(p.Dir, p.Cfg, "taken", "projections/taken.js", "all", "none", false)
	if err == nil {
		t.Fatal("expected error for existing file")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestScaffold_PathValidation(t *testing.T) {
	p := testutil.NewProject(t).Save()

	cases := []struct {
		name string
		path string
		want string
	}{
		{"empty path", "", "path is required"},
		{"whitespace path", "   ", "path is required"},
		{"missing extension", "counter", "must end in"},
		{"unsupported extension", "counter.go", "must end in"},
		{"absolute path", "/etc/counter.js", "must be relative"},
		{"parent traversal", "../counter.js", "outside the project root"},
		{"parent traversal nested", "sub/../../counter.js", "outside the project root"},
		{"extension-only", ".js", "missing a file name"},
		{"extension-only in subdir", "sub/.js", "missing a file name"},
		{"backslash parent traversal", "..\\counter.js", "outside the project root"},
		{"windows drive-letter absolute", "C:\\tmp\\counter.js", "must be relative"},
		{"windows drive-letter forward slash", "C:/tmp/counter.js", "must be relative"},
		{"windows drive-relative", "C:counter.js", "must be relative"},
		{"windows drive-letter lowercase", "d:\\counter.js", "must be relative"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Scaffold(p.Dir, p.Cfg, "x", tc.path, "all", "none", false)
			if err == nil {
				t.Fatalf("expected error for path %q", tc.path)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("expected error containing %q, got: %v", tc.want, err)
			}
		})
	}
}

func TestScaffold_BackslashSeparatorsNormalised(t *testing.T) {
	// Backslashes get normalised to forward slashes so the toml
	// entry stays slash-form and portable across platforms.
	p := testutil.NewProject(t).Save()

	result, err := Scaffold(p.Dir, p.Cfg, "totals", "lib\\handlers\\totals.js", "all", "none", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.RelPath != "lib/handlers/totals.js" {
		t.Errorf("relPath: got %q, want slash-form 'lib/handlers/totals.js'", result.RelPath)
	}
}

func TestScaffold_NameDefaultsFromBasename(t *testing.T) {
	// Name defaulting lives in scaffold.Scaffold; passing "" picks
	// the basename without extension.
	p := testutil.NewProject(t).Save()

	result, err := Scaffold(p.Dir, p.Cfg, "", "projections/counter.js", "all", "none", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "counter" {
		t.Errorf("default name: got %q, want %q", result.Name, "counter")
	}
}

func TestScaffold_DotPrefixFilenameRoundTripsThroughLoad(t *testing.T) {
	// A filename whose stem starts with ".." (e.g. "..hidden.js") is
	// a legitimate name, not parent traversal. Scaffold must accept
	// it AND the resulting gaffer.toml must reload cleanly - if
	// either side rejects the entry on a literal-prefix match, the
	// CLI writes a toml that subsequent commands can't load.
	p := testutil.NewProject(t).Save()

	result, err := Scaffold(p.Dir, p.Cfg, "", "..hidden.js", "all", "none", false)
	if err != nil {
		t.Fatal(err)
	}
	if result.RelPath != "..hidden.js" {
		t.Errorf("relPath: got %q, want %q", result.RelPath, "..hidden.js")
	}

	reloaded, err := config.Load(filepath.Join(p.Dir, "gaffer.toml"))
	if err != nil {
		t.Fatalf("expected reload to succeed for a literal '..'-prefixed filename, got: %v", err)
	}
	if reloaded.FindProjection("..hidden") == nil {
		t.Error("expected ..hidden projection in reloaded config")
	}
}

func TestScaffold_RejectsSymlinkEscape(t *testing.T) {
	// A symlink inside the project tree pointing outside must not
	// bypass the no-escape check. Lexical validation passes (the
	// path is "inside" textually); the symlink resolution catches it.
	p := testutil.NewProject(t).Save()
	outside := t.TempDir()
	linkInRoot := filepath.Join(p.Dir, "escape")
	if err := os.Symlink(outside, linkInRoot); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}

	_, err := Scaffold(p.Dir, p.Cfg, "x", "escape/counter.js", "all", "none", false)
	if err == nil {
		t.Fatal("expected error for symlink escape")
	}
	if !strings.Contains(err.Error(), "outside the project root") {
		t.Errorf("expected 'outside the project root', got: %v", err)
	}
}

func TestScaffold_RejectsDeepSymlinkEscape(t *testing.T) {
	// Symlink sits more than one level above the missing leaf.
	// resolveAncestorSymlinks must walk past the not-yet-existing
	// directories to find and resolve the symlink.
	p := testutil.NewProject(t).Save()
	outside := t.TempDir()
	linkInRoot := filepath.Join(p.Dir, "escape")
	if err := os.Symlink(outside, linkInRoot); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}

	// Two levels of missing directories between the symlink and
	// the leaf - exercises the loop, not just the single-step fallback.
	_, err := Scaffold(p.Dir, p.Cfg, "x", "escape/new-sub/nested/counter.js", "all", "none", false)
	if err == nil {
		t.Fatal("expected error for deep symlink escape")
	}
	if !strings.Contains(err.Error(), "outside the project root") {
		t.Errorf("expected 'outside the project root', got: %v", err)
	}
}

func TestScaffold_NameValidation(t *testing.T) {
	p := testutil.NewProject(t).Save()

	_, err := Scaffold(p.Dir, p.Cfg, "  ", "projections/x.js", "all", "none", false)
	if err == nil {
		t.Fatal("expected error for whitespace-only name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("expected 'name is required', got: %v", err)
	}
}

func TestEscapeJS(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"it's", `it\'s`},
		{`back\slash`, `back\\slash`},
		{"'); malicious('", `\'); malicious(\'`},
	}

	for _, tt := range tests {
		got := escapeJS(tt.input)
		if got != tt.expected {
			t.Errorf("escapeJS(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestGenerateSource(t *testing.T) {
	tests := []struct {
		name      string
		source    string
		partition string
		emit      bool
		golden    string
		errMsg    string
	}{
		{
			name:      "fromAll no partition",
			source:    "all",
			partition: "none",
			golden:    "generate_all_none",
		},
		{
			name:      "fromCategory per-stream",
			source:    "category:orders",
			partition: "per-stream",
			golden:    "generate_category_perstream",
		},
		{
			name:      "fromStream",
			source:    "stream:my-stream",
			partition: "none",
			golden:    "generate_stream",
		},
		{
			name:      "with emit",
			source:    "all",
			partition: "none",
			emit:      true,
			golden:    "generate_all_emit",
		},
		{
			name:      "escapes single quotes",
			source:    "stream:it's-a-stream",
			partition: "none",
			golden:    "generate_stream_escaped",
		},
		{
			name:      "invalid partition",
			source:    "all",
			partition: "custom",
			errMsg:    "unsupported partition",
		},
		{
			name:      "invalid source",
			source:    "topic:foo",
			partition: "none",
			errMsg:    "unsupported source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GenerateSource(tt.source, tt.partition, tt.emit)
			if tt.errMsg != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Fatalf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatal(err)
			}

			testutil.AssertGolden(t, tt.golden, result)
		})
	}
}
