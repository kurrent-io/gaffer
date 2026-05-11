package telemetry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

func TestLoadTelemetry_Missing(t *testing.T) {
	dir := t.TempDir()
	s, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, err := LoadTelemetry(s)
	if err != nil {
		t.Fatalf("LoadTelemetry: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("LoadTelemetry on empty store = %+v, want zero", got)
	}
}

func TestLoadTelemetry_PerFieldToleranceKeepsConsent(t *testing.T) {
	// User hand-edits enabled = 1 expecting it to mean true. The
	// preference is malformed, but ID and Salt parsed cleanly; the
	// usable fields survive so a subsequent gaffer run can still
	// emit/clear via the persisted secrets.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	const bad = `[telemetry]
enabled = 1
id = "abc"
salt = "def"
`
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, err := LoadTelemetry(s)
	if err == nil {
		t.Error("LoadTelemetry on enabled=1 returned nil err, want field error")
	}
	if got.ID != "abc" {
		t.Errorf("ID dropped: got %q, want abc", got.ID)
	}
	if got.Salt != "def" {
		t.Errorf("Salt dropped: got %q, want def", got.Salt)
	}
	if got.Enabled != nil {
		t.Errorf("Enabled = %v, want nil (malformed value dropped)", got.Enabled)
	}
}

func TestLoadTelemetry_PerFieldToleranceConsentSurvivesBadSalt(t *testing.T) {
	// Inverse: id/salt malformed but Enabled valid. The consent bit
	// is the only field with real user intent (id/salt regenerate);
	// it survives so opt-out cascades still work.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	const bad = `[telemetry]
enabled = false
id = 42
salt = 99
`
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, err := LoadTelemetry(s)
	if err == nil {
		t.Error("LoadTelemetry returned nil err, want field errors")
	}
	if got.Enabled == nil || *got.Enabled != false {
		t.Errorf("Enabled lost: got %v, want &false", got.Enabled)
	}
}

func TestLoadTelemetry_RejectsScalarSection(t *testing.T) {
	// Top-level `telemetry = "off"` (scalar instead of table) is a
	// structural error - Section returns nil, looks like absent.
	// LoadTelemetry surfaces a distinct error so the caller can warn
	// rather than silently re-mint.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	const bad = `telemetry = "off"
`
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = LoadTelemetry(s)
	if err == nil {
		t.Error("LoadTelemetry on scalar [telemetry] returned nil err")
	}
}

func TestWriteAndLoad_TelemetryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	on := true
	WriteTelemetry(s, TelemetrySection{
		Enabled: &on,
		ID:      "00000000-0000-0000-0000-000000000001",
		Salt:    "00000000-0000-0000-0000-000000000002",
	})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	s2, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	got, err := LoadTelemetry(s2)
	if err != nil {
		t.Fatalf("LoadTelemetry: %v", err)
	}
	if got.Enabled == nil || *got.Enabled != true {
		t.Errorf("Enabled = %v, want &true", got.Enabled)
	}
	if got.ID != "00000000-0000-0000-0000-000000000001" {
		t.Errorf("ID = %s", got.ID)
	}
	if got.Salt != "00000000-0000-0000-0000-000000000002" {
		t.Errorf("Salt = %s", got.Salt)
	}
}

func TestWriteTelemetry_ZeroRemovesSection(t *testing.T) {
	dir := t.TempDir()
	s, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	WriteTelemetry(s, TelemetrySection{ID: "x"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save 1: %v", err)
	}

	s2, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	WriteTelemetry(s2, TelemetrySection{})
	if err := s2.Save(); err != nil {
		t.Fatalf("Save 2: %v", err)
	}

	s3, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Reload 2: %v", err)
	}
	got, err := LoadTelemetry(s3)
	if err != nil {
		t.Fatalf("LoadTelemetry: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("section persisted after WriteTelemetry(zero): %+v", got)
	}
}

func TestTelemetrySectionStatus_RedactsSalt(t *testing.T) {
	on := true
	t1 := TelemetrySection{Enabled: &on, ID: "abc-123", Salt: "secret-salt"}
	got := t1.Status()
	if !strings.Contains(got, "enabled=true") {
		t.Errorf("Status missing enabled=true: %s", got)
	}
	if !strings.Contains(got, "id=abc-123") {
		t.Errorf("Status missing id: %s", got)
	}
	if strings.Contains(got, "secret-salt") {
		t.Errorf("Status leaked salt: %s", got)
	}
	if !strings.Contains(got, "salt=<redacted>") {
		t.Errorf("Status missing redacted-salt marker: %s", got)
	}
}

func TestTelemetrySectionStatus_UnsetEnabled(t *testing.T) {
	got := TelemetrySection{ID: "abc"}.Status()
	if !strings.Contains(got, "enabled=unset") {
		t.Errorf("Status missing enabled=unset: %s", got)
	}
}

func TestClearTelemetry_RemovesSection(t *testing.T) {
	dir := t.TempDir()
	s, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	on := true
	WriteTelemetry(s, TelemetrySection{Enabled: &on, ID: "x", Salt: "y"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	s2, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	ClearTelemetry(s2)
	if err := s2.Save(); err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	s3, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Reload 2: %v", err)
	}
	got, err := LoadTelemetry(s3)
	if err != nil {
		t.Fatalf("LoadTelemetry: %v", err)
	}
	if !got.IsZero() {
		t.Errorf("section persisted after ClearTelemetry: %+v", got)
	}
}

func TestWriteTelemetry_RoundTripEnabledOnly(t *testing.T) {
	dir := t.TempDir()
	s, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	off := false
	WriteTelemetry(s, TelemetrySection{Enabled: &off})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	s2, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	got, err := LoadTelemetry(s2)
	if err != nil {
		t.Fatalf("LoadTelemetry: %v", err)
	}
	if got.Enabled == nil || *got.Enabled != false {
		t.Errorf("Enabled = %v, want &false", got.Enabled)
	}
	if got.ID != "" || got.Salt != "" {
		t.Errorf("ID/Salt set unexpectedly: %+v", got)
	}
}
