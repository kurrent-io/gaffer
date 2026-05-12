package telemetry

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

var (
	uuidPattern  = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	hexIDPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

func TestMintIdentity_AllUUIDFormat(t *testing.T) {
	id, err := MintIdentity()
	if err != nil {
		t.Fatalf("MintIdentity: %v", err)
	}
	for _, tc := range []struct {
		name string
		val  string
	}{
		{"TelemetryID", id.TelemetryID},
		{"Salt", id.Salt},
		{"RunID", id.RunID},
	} {
		if !uuidPattern.MatchString(tc.val) {
			t.Errorf("%s = %q, not a lowercase-hyphen UUID", tc.name, tc.val)
		}
	}
}

func TestMintIdentity_ValuesAreDistinct(t *testing.T) {
	id, err := MintIdentity()
	if err != nil {
		t.Fatalf("MintIdentity: %v", err)
	}
	if id.TelemetryID == id.Salt || id.TelemetryID == id.RunID || id.Salt == id.RunID {
		t.Errorf("expected distinct UUIDs, got %+v", id)
	}
}

func TestIdentity_IsZero(t *testing.T) {
	var zero Identity
	if !zero.IsZero() {
		t.Errorf("zero Identity IsZero = false")
	}
	id := Identity{TelemetryID: "x"}
	if id.IsZero() {
		t.Errorf("populated Identity IsZero = true")
	}
}

func TestIdentityFromConfig_MissingReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	s, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, ok, err := IdentityFromConfig(s)
	if err != nil {
		t.Fatalf("IdentityFromConfig: %v", err)
	}
	if ok {
		t.Errorf("ok = true on empty config, want false")
	}
}

func TestIdentityFromConfig_PartialReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	s, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	WriteTelemetry(s, TelemetrySection{ID: "00000000-0000-0000-0000-000000000001"})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	s2, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	_, ok, err := IdentityFromConfig(s2)
	if err != nil {
		t.Fatalf("IdentityFromConfig: %v", err)
	}
	if ok {
		t.Errorf("ok = true on partial config (ID without Salt), want false")
	}
}

func TestStageAndLoadIdentity(t *testing.T) {
	dir := t.TempDir()
	s, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	id, err := MintIdentity()
	if err != nil {
		t.Fatalf("MintIdentity: %v", err)
	}
	StageIdentity(s, id)
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	s2, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	got, ok, err := IdentityFromConfig(s2)
	if err != nil {
		t.Fatalf("IdentityFromConfig: %v", err)
	}
	if !ok {
		t.Fatal("ok = false after stage+save")
	}
	if got.TelemetryID != id.TelemetryID {
		t.Errorf("TelemetryID = %s, want %s", got.TelemetryID, id.TelemetryID)
	}
	if got.Salt != id.Salt {
		t.Errorf("Salt = %s, want %s", got.Salt, id.Salt)
	}
	// RunID must be freshly minted, NOT the previously persisted one.
	if got.RunID == id.RunID {
		t.Errorf("RunID was carried over from config (must be fresh per process)")
	}
}

func TestStageIdentity_PreservesEnabledFlag(t *testing.T) {
	dir := t.TempDir()
	s, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	on := true
	WriteTelemetry(s, TelemetrySection{Enabled: &on})

	id, err := MintIdentity()
	if err != nil {
		t.Fatalf("MintIdentity: %v", err)
	}
	StageIdentity(s, id)
	got, err := LoadTelemetry(s)
	if err != nil {
		t.Fatalf("LoadTelemetry: %v", err)
	}
	if got.Enabled == nil || *got.Enabled != true {
		t.Errorf("StageIdentity cleared Enabled; got %v", got.Enabled)
	}
}

func TestClearIdentity_ReturnsPriorIDAndRemovesSecretsOnly(t *testing.T) {
	dir := t.TempDir()
	s, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	off := false
	WriteTelemetry(s, TelemetrySection{
		Enabled: &off,
		ID:      "00000000-0000-0000-0000-000000000abc",
		Salt:    "00000000-0000-0000-0000-000000000def",
	})

	cleared := ClearIdentity(s)
	if cleared != "00000000-0000-0000-0000-000000000abc" {
		t.Errorf("cleared = %s, want the previous ID", cleared)
	}
	got, err := LoadTelemetry(s)
	if err != nil {
		t.Fatalf("LoadTelemetry: %v", err)
	}
	if got.ID != "" || got.Salt != "" {
		t.Errorf("Clear left residue: ID=%q Salt=%q", got.ID, got.Salt)
	}
	if got.Enabled == nil || *got.Enabled != false {
		t.Errorf("Clear cleared Enabled too; got %v", got.Enabled)
	}
}

func TestClearIdentity_OnEmptyConfigReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	s, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cleared := ClearIdentity(s)
	if cleared != "" {
		t.Errorf("cleared = %q, want \"\" on empty config", cleared)
	}
}

func TestProjectID_DeterministicAndHexFormat(t *testing.T) {
	salt := "deadbeef-cafe-0000-0000-000000000000"
	a := ProjectID(salt, "/abs/project/path")
	b := ProjectID(salt, "/abs/project/path")
	if a != b {
		t.Errorf("ProjectID not deterministic: %s vs %s", a, b)
	}
	if !hexIDPattern.MatchString(a) {
		t.Errorf("ProjectID = %s, want 16 lowercase hex chars", a)
	}
}

func TestProjectionID_DeterministicAndHexFormat(t *testing.T) {
	salt := "deadbeef-cafe-0000-0000-000000000000"
	a := ProjectionID(salt, "/abs/projection.js")
	b := ProjectionID(salt, "/abs/projection.js")
	if a != b {
		t.Errorf("ProjectionID not deterministic: %s vs %s", a, b)
	}
	if !hexIDPattern.MatchString(a) {
		t.Errorf("ProjectionID = %s, want 16 lowercase hex chars", a)
	}
}

func TestDeriveID_HMACDomainSeparation(t *testing.T) {
	// HMAC keys the hash by salt, so the salt||path concatenation
	// collision that plain sha256 would have - sha256("ab", "cdef") ==
	// sha256("abcd", "ef") - is impossible.
	if deriveID("foo", "bar") == deriveID("foobar", "") {
		t.Error("HMAC didn't provide domain separation")
	}
	if deriveID("ab", "cdef") == deriveID("abcd", "ef") {
		t.Error("HMAC didn't provide domain separation")
	}
}

func TestDeriveID_PathSensitive(t *testing.T) {
	salt := "deadbeef-cafe-0000-0000-000000000000"
	if deriveID(salt, "/a") == deriveID(salt, "/b") {
		t.Error("deriveID collides on different paths")
	}
}

func TestDeriveID_SaltSensitive(t *testing.T) {
	if deriveID("salt1", "/path") == deriveID("salt2", "/path") {
		t.Error("deriveID collides on different salts")
	}
}

func TestDeriveID_NoCollisionsInSyntheticSet(t *testing.T) {
	// Smoke test: 10k synthetic paths under one salt must all map to
	// distinct 16-hex-char IDs. Catches a future regression that
	// accidentally truncates further or breaks the HMAC keying.
	salt := "deadbeef-cafe-0000-0000-000000000000"
	seen := make(map[string]struct{}, 10_000)
	for i := 0; i < 10_000; i++ {
		path := fmt.Sprintf("/projects/p%d/projection.js", i)
		id := deriveID(salt, path)
		if _, dup := seen[id]; dup {
			t.Fatalf("collision after %d paths: %s", i, id)
		}
		seen[id] = struct{}{}
	}
}

func TestIdentity_Status(t *testing.T) {
	var zero Identity
	if got := zero.Status(); got != "identity=unset" {
		t.Errorf("zero Identity Status = %q, want \"identity=unset\"", got)
	}
	id := Identity{TelemetryID: "tid-abc", Salt: "secret-salt", RunID: "run-xyz"}
	got := id.Status()
	if !strings.Contains(got, "tid-abc") {
		t.Errorf("Status missing TelemetryID: %q", got)
	}
	if !strings.Contains(got, "run-xyz") {
		t.Errorf("Status missing RunID: %q", got)
	}
	if strings.Contains(got, "secret-salt") {
		t.Errorf("Status leaked Salt: %q", got)
	}
	if !strings.Contains(got, "salt=<redacted>") {
		t.Errorf("Status missing redacted-salt marker: %q", got)
	}
}

func TestIdentityFromConfig_PropagatesPartialError(t *testing.T) {
	// User has valid id/salt but malformed enabled. IdentityFromConfig
	// returns the usable identity AND surfaces the warning so the
	// caller (commit 5 / commit 6) can advise the user to fix the file.
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
	id, ok, loadErr := IdentityFromConfig(s)
	if !ok {
		t.Fatal("ok = false, want true (id/salt parsed)")
	}
	if id.TelemetryID != "abc" || id.Salt != "def" {
		t.Errorf("id = %+v, want TelemetryID=abc Salt=def", id)
	}
	if loadErr == nil {
		t.Error("loadErr = nil, want surfaced field warning")
	}
}

func TestIdentityFromConfig_PartialUsableFalseSurfacesError(t *testing.T) {
	// Inverse: salt malformed → no usable identity. Caller sees
	// (zero, false, err).
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	const bad = `[telemetry]
id = "abc"
salt = 99
`
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, ok, loadErr := IdentityFromConfig(s)
	if ok {
		t.Error("ok = true, want false (salt unusable)")
	}
	if loadErr == nil {
		t.Error("loadErr = nil, want surfaced field error")
	}
}

func TestStageIdentity_OverwritesExistingID(t *testing.T) {
	dir := t.TempDir()
	s, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	first := Identity{TelemetryID: "first-id", Salt: "first-salt"}
	StageIdentity(s, first)
	second := Identity{TelemetryID: "second-id", Salt: "second-salt"}
	StageIdentity(s, second)
	got, err := LoadTelemetry(s)
	if err != nil {
		t.Fatalf("LoadTelemetry: %v", err)
	}
	if got.ID != "second-id" || got.Salt != "second-salt" {
		t.Errorf("Stage didn't overwrite: %+v", got)
	}
}

func TestClearIdentity_PreservesEnabledForReMint(t *testing.T) {
	// Scenario: user opted in (Enabled=true), CLI minted (id+salt
	// set). Then ClearIdentity runs (e.g. RTBF flow paired with a
	// re-opt-in later). Enabled survives so a subsequent run that
	// finds id/salt empty but Enabled=true mints fresh secrets while
	// honouring the user's standing opt-in.
	dir := t.TempDir()
	s, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	on := true
	WriteTelemetry(s, TelemetrySection{
		Enabled: &on,
		ID:      "old-id",
		Salt:    "old-salt",
	})

	cleared := ClearIdentity(s)
	if cleared != "old-id" {
		t.Errorf("cleared = %q, want old-id", cleared)
	}
	got, err := LoadTelemetry(s)
	if err != nil {
		t.Fatalf("LoadTelemetry: %v", err)
	}
	if got.Enabled == nil || *got.Enabled != true {
		t.Errorf("Enabled lost after Clear: %v", got.Enabled)
	}
	if got.ID != "" || got.Salt != "" {
		t.Errorf("secrets persisted after Clear: %+v", got)
	}

	// Next "run": IdentityFromConfig sees no id/salt → false →
	// caller mints fresh. Enabled remains true so opt-in survives.
	_, ok, _ := IdentityFromConfig(s)
	if ok {
		t.Error("IdentityFromConfig returned true after Clear; want false")
	}
}
