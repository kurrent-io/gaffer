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

	result, err := Scaffold(p.Dir, p.Cfg, "counter", "category:order", "per-stream", true)
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

func TestScaffold_DuplicateName(t *testing.T) {
	p := testutil.NewProject(t).AddProjection("existing", "// placeholder").Save()

	_, err := Scaffold(p.Dir, p.Cfg, "existing", "all", "none", false)
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

	_, err := Scaffold(p.Dir, p.Cfg, "taken", "all", "none", false)
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

			testutil.AssertGolden(t, tt.golden, result)
		})
	}
}
