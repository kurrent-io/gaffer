package scaffold

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

var update = flag.Bool("update", false, "update golden files")

func assertGolden(t *testing.T, name, actual string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")

	if *update {
		if err := os.WriteFile(path, []byte(actual), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden file %s (run with -update to create)", path)
	}
	if actual != string(expected) {
		t.Errorf("output does not match %s\n\ngot:\n%s\nwant:\n%s", path, actual, expected)
	}
}

func setupProject(t *testing.T) (string, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Projection: []config.Projection{
			{Name: "existing", Entry: "projections/existing.js"},
		},
	}
	if err := config.Save(filepath.Join(dir, "gaffer.toml"), cfg); err != nil {
		t.Fatal(err)
	}
	return dir, cfg
}

func TestScaffold(t *testing.T) {
	dir, cfg := setupProject(t)

	result, err := Scaffold(dir, cfg, "counter", "category:order", "per-stream", true)
	if err != nil {
		t.Fatal(err)
	}

	if result.Name != "counter" {
		t.Errorf("name: got %q, want %q", result.Name, "counter")
	}
	if result.RelPath != "projections/counter.js" {
		t.Errorf("relPath: got %q, want %q", result.RelPath, "projections/counter.js")
	}

	content, err := os.ReadFile(filepath.Join(dir, "projections/counter.js"))
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "scaffold_category_perstream_emit", string(content))

	reloaded, err := config.Load(filepath.Join(dir, "gaffer.toml"))
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

func TestScaffold_DuplicateName(t *testing.T) {
	dir, cfg := setupProject(t)

	_, err := Scaffold(dir, cfg, "existing", "all", "none", false)
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestScaffold_FileAlreadyExists(t *testing.T) {
	dir, cfg := setupProject(t)

	projDir := filepath.Join(dir, "projections")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "taken.js"), []byte("//"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Scaffold(dir, cfg, "taken", "all", "none", false)
	if err == nil {
		t.Fatal("expected error for existing file")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
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

			assertGolden(t, tt.golden, result)
		})
	}
}
