package telemetry

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"

	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

// Identity holds the per-install secrets and the per-process run id.
// TelemetryID and Salt persist across runs (in the user config);
// RunID is generated fresh per process and never written to disk.
type Identity struct {
	// TelemetryID is the per-install random UUID sent as `emitter_id`
	// on every envelope. Persisted in [telemetry].id.
	TelemetryID string

	// Salt is the per-install random UUID used to derive project_id /
	// projection_id hashes. Persisted in [telemetry].salt. Never sent
	// over the wire.
	Salt string

	// RunID is a fresh UUID per CLI invocation. In-memory only; sent
	// as `run_id` on every envelope from this process.
	RunID string
}

// IsZero reports whether the identity is unset (no TelemetryID minted).
func (i Identity) IsZero() bool { return i.TelemetryID == "" }

// Status summarises the identity for debug logging. Like
// TelemetrySection.Status, it is *not* called by %v so that a future
// log-scrubber can rely on field-shape output for the bare struct;
// callers wanting redacted text invoke Status explicitly. Salt is
// redacted (it's a real secret used in HMAC derivation); ID and RunID
// are shown since the ID is the RTBF deletion handle the user prints
// from `gaffer config telemetry off` anyway.
func (i Identity) Status() string {
	if i.IsZero() {
		return "identity=unset"
	}
	return fmt.Sprintf("telemetry_id=%s run_id=%s salt=<redacted>", i.TelemetryID, i.RunID)
}

// MintIdentity generates fresh TelemetryID and Salt UUIDs plus a RunID.
// The returned Identity is the one the current process should use; if
// the caller goes on to StageIdentity + Save and the Save loses the
// first-write race, the caller must Reload, re-derive the persisted
// halves via IdentityFromConfig, and discard the minted TelemetryID +
// Salt - but keep the freshly minted RunID for the current process.
func MintIdentity() (Identity, error) {
	tid, err := uuid.NewRandom()
	if err != nil {
		return Identity{}, fmt.Errorf("mint telemetry_id: %w", err)
	}
	salt, err := uuid.NewRandom()
	if err != nil {
		return Identity{}, fmt.Errorf("mint salt: %w", err)
	}
	run, err := uuid.NewRandom()
	if err != nil {
		return Identity{}, fmt.Errorf("mint run_id: %w", err)
	}
	return Identity{
		TelemetryID: tid.String(),
		Salt:        salt.String(),
		RunID:       run.String(),
	}, nil
}

// IdentityFromConfig pulls the persistent halves out of the user
// config's [telemetry] section and pairs them with a fresh RunID.
//
// Returns (identity, true, err) when both TelemetryID and Salt parsed.
// Returns (zero, false, err) when either is missing.
//
// The err return surfaces any per-field type mismatches from
// LoadTelemetry. A non-nil err with usable=true means "you have an
// identity, but some other field (likely Enabled) was malformed" -
// caller can use the identity and surface the warning. A non-nil err
// with usable=false means "no usable identity AND something was
// malformed" - typically the salt or id itself failed to parse.
func IdentityFromConfig(s *userconfig.Store) (Identity, bool, error) {
	t, loadErr := LoadTelemetry(s)
	if t.ID == "" || t.Salt == "" {
		return Identity{}, false, loadErr
	}
	run, err := uuid.NewRandom()
	if err != nil {
		return Identity{}, false, fmt.Errorf("mint run_id: %w", err)
	}
	return Identity{
		TelemetryID: t.ID,
		Salt:        t.Salt,
		RunID:       run.String(),
	}, true, loadErr
}

// StageIdentity writes the persistent halves of id into s's [telemetry]
// section. Does not touch Enabled. Caller must call s.Save() to
// persist; "Stage" rather than "Persist" because the change is
// in-memory until then.
//
// Returns any error LoadTelemetry surfaced for the pre-existing
// section (structural or per-field) so callers can warn. The write
// proceeds regardless: a malformed Enabled in the file will be
// dropped when WriteTelemetry round-trips through TelemetrySection.
// That's intentional - StageIdentity runs in the first-mint flow
// where the user hasn't engaged with the CLI yet, so preserving a
// malformed user edit isn't a goal.
func StageIdentity(s *userconfig.Store, id Identity) error {
	t, loadErr := LoadTelemetry(s)
	t.ID = id.TelemetryID
	t.Salt = id.Salt
	WriteTelemetry(s, t)
	return loadErr
}

// ClearIdentity removes TelemetryID and Salt from s's [telemetry]
// section. Returns the cleared TelemetryID (so the caller can print
// it one last time for RTBF disclosure: `gaffer config telemetry off`)
// plus any error LoadTelemetry surfaced for the pre-existing section.
// Enabled is preserved - the user explicitly opted out, the
// preference outlives the secret. Caller must call s.Save().
func ClearIdentity(s *userconfig.Store) (string, error) {
	t, loadErr := LoadTelemetry(s)
	cleared := t.ID
	t.ID = ""
	t.Salt = ""
	WriteTelemetry(s, t)
	return cleared, loadErr
}

// deriveID is the shared HMAC-SHA256 primitive used by ProjectID and
// ProjectionID. Returns 16 lowercase hex characters (8 bytes of the
// 32-byte digest); see project notes for the collision-space argument
// (gaffer-scale unique IDs collide at single-digit-ppm rates well
// below realistic install counts).
func deriveID(salt, absPath string) string {
	mac := hmac.New(sha256.New, []byte(salt))
	_, _ = mac.Write([]byte(absPath))
	sum := mac.Sum(nil)
	return hex.EncodeToString(sum[:8])
}

// ProjectID returns the wire-format project_id for the given salt and
// absolute project-root path. Callers must pass a cleaned, absolute
// path (filepath.Abs + filepath.Clean) so the same project hashes
// consistently across runs.
func ProjectID(salt, absProjectRoot string) string {
	return deriveID(salt, absProjectRoot)
}

// ProjectionID returns the wire-format projection_id for the given salt
// and absolute projection-file path. Same path-normalisation
// expectations as ProjectID.
func ProjectionID(salt, absProjectionFile string) string {
	return deriveID(salt, absProjectionFile)
}
