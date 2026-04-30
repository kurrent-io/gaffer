package testutil

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kurrent-io/gaffer/cli/internal/config"
)

var update = flag.Bool("update", false, "update golden files")

// --- Assertions ---

func AssertEqual[T comparable](t *testing.T, name string, want, got T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

func AssertEqualFloat(t *testing.T, name string, want float64, got any) {
	t.Helper()
	f, ok := got.(float64)
	if !ok {
		t.Errorf("%s: expected float64, got %T", name, got)
		return
	}
	if f != want {
		t.Errorf("%s: got %v, want %v", name, f, want)
	}
}

func AssertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected output to contain %q, got:\n%s", needle, haystack)
	}
}

// --- Golden files ---

// AssertGolden compares actual against a golden file in the calling
// package's testdata/ directory. Run tests with -update to regenerate.
func AssertGolden(t *testing.T, name, actual string) {
	t.Helper()
	AssertGoldenDir(t, "testdata", name, actual)
}

// AssertGoldenDir compares actual against a golden file in the given directory.
func AssertGoldenDir(t *testing.T, dir, name, actual string) {
	t.Helper()
	path := filepath.Join(dir, name+".golden")

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

// --- Test events ---

// Event builds a JSON event string suitable for feeding to the runtime.
func Event(eventType, streamID string, seq int) string {
	return fmt.Sprintf(
		`{"eventType":%q,"streamId":%q,"sequenceNumber":%d,"data":"{}","isJson":true,"eventId":"00000000-0000-0000-0000-%012d","created":"2026-01-01T00:00:00Z"}`,
		eventType, streamID, seq, seq,
	)
}

// --- Test project ---

// Project creates a temp directory with a gaffer.toml.
type Project struct {
	Dir string
	Cfg *config.Config
	t   *testing.T
}

// NewProject returns a test project with engine_version=2 by default.
// Call WithEngineVersion to override.
func NewProject(t *testing.T) *Project {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{EngineVersion: 2}
	return &Project{Dir: dir, Cfg: cfg, t: t}
}

func (p *Project) WithEngineVersion(v int) *Project {
	p.Cfg.EngineVersion = v
	return p
}

func (p *Project) WithConnection(connStr string) *Project {
	p.Cfg.Connection = connStr
	return p
}

func (p *Project) AddProjection(name, source string) *Project {
	p.t.Helper()
	relPath := filepath.Join("projections", name+".js")
	absPath := filepath.Join(p.Dir, relPath)

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		p.t.Fatal(err)
	}
	if err := os.WriteFile(absPath, []byte(source), 0o644); err != nil {
		p.t.Fatal(err)
	}

	p.Cfg.Projection = append(p.Cfg.Projection, config.Projection{
		Name:  name,
		Entry: relPath,
	})
	return p
}

func (p *Project) AddFixture(name, content string) *Project {
	p.t.Helper()
	absPath := filepath.Join(p.Dir, "fixtures", name+".json")

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		p.t.Fatal(err)
	}
	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		p.t.Fatal(err)
	}
	return p
}

func (p *Project) Save() *Project {
	p.t.Helper()
	if err := config.Save(filepath.Join(p.Dir, "gaffer.toml"), p.Cfg); err != nil {
		p.t.Fatal(err)
	}
	return p
}

// --- Integration helpers ---

// ConnectionString returns KURRENTDB_URL from the environment,
// or the default insecure localhost connection.
func ConnectionString() string {
	if s := os.Getenv("KURRENTDB_URL"); s != "" {
		return s
	}
	return "kurrentdb://localhost:2113?tls=false"
}

// TestSuffix returns a short random string for unique stream/category names.
func TestSuffix() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")[:12]
}

// --- Output helpers ---

// CaptureStdout redirects os.Stdout for the duration of fn and returns
// what was written. Safe against panics - stdout is always restored.
func CaptureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	_ = w.Close()

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.String()
}

// SplitNDJSON parses newline-delimited JSON into a slice of maps.
func SplitNDJSON(s string) []map[string]any {
	var results []map[string]any
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	for dec.More() {
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			break
		}
		results = append(results, obj)
	}
	return results
}
