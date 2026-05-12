package telemetry

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

// TelemetrySection mirrors the on-disk [telemetry] block in the
// userconfig.Store. *Enabled has three states - nil (no explicit choice
// yet), true (opted in), false (opted out) - matching the three-layer
// status output planned for `gaffer config telemetry status`.
//
// Disclosed records whether a user-facing disclosure has been shown
// (either by gaffer's first-mint stderr notice, or by an upstream
// surface like the VS Code extension that ran its own disclosure flow
// and then called `gaffer config telemetry on --quiet`). Notice
// suppression keys on this flag, not on the --invoker-id spawn-link
// flag - so an unsupervised wrapper can't silence disclosure just by
// passing a fake invoker id.
type TelemetrySection struct {
	Enabled   *bool
	ID        string
	Salt      string
	Disclosed bool
}

// IsZero reports whether the section carries no information.
func (t TelemetrySection) IsZero() bool {
	return t.Enabled == nil && t.ID == "" && t.Salt == "" && !t.Disclosed
}

// Status summarises the section for debug logging and the `gaffer
// config telemetry status` output. Named Status (not String) so %v
// formatting walks the struct fields explicitly rather than calling a
// custom stringer that future log scrubbers would have to allow-list.
func (t TelemetrySection) Status() string {
	var parts []string
	switch {
	case t.Enabled == nil:
		parts = append(parts, "enabled=unset")
	case *t.Enabled:
		parts = append(parts, "enabled=true")
	default:
		parts = append(parts, "enabled=false")
	}
	if t.ID != "" {
		parts = append(parts, "id="+t.ID)
	}
	if t.Salt != "" {
		parts = append(parts, "salt=<redacted>")
	}
	return strings.Join(parts, " ")
}

// LoadTelemetry pulls the [telemetry] section out of a userconfig.Store.
//
// Per-field tolerance: a type mismatch on one field (e.g. user hand-
// edited `enabled = 1` expecting it to mean true) does NOT discard the
// other fields - the returned section is best-effort populated with
// whatever parsed cleanly, and the returned error wraps the per-field
// issues. This matters because Enabled is the only field carrying real
// user intent (id/salt can be re-minted); losing consent because
// somebody typoed salt would be hostile.
//
// The returned error is non-nil for:
//   - per-field type mismatches (other fields still populated)
//   - structural error: [telemetry] exists but is a scalar, not a table
//
// Callers that need consent state at all costs read t.Enabled even
// when err != nil. Callers that need integrity (e.g. config-telemetry
// on/off about to write) should abort on err != nil.
func LoadTelemetry(s *userconfig.Store) (TelemetrySection, error) {
	present, isTable := s.SectionPresent("telemetry")
	if present && !isTable {
		return TelemetrySection{}, errors.New("user config: [telemetry] is set to a non-table value (e.g. `telemetry = \"off\"`); remove or rewrite as a table")
	}
	section := s.Section("telemetry")
	if section == nil {
		return TelemetrySection{}, nil
	}

	var t TelemetrySection
	var errs []error
	if raw, ok := section["enabled"]; ok {
		b, isBool := raw.(bool)
		if isBool {
			t.Enabled = &b
		} else {
			errs = append(errs, fmt.Errorf("[telemetry] enabled must be a boolean, got %T (%v)", raw, raw))
		}
	}
	if raw, ok := section["id"]; ok {
		str, isStr := raw.(string)
		if isStr {
			t.ID = str
		} else {
			errs = append(errs, fmt.Errorf("[telemetry] id must be a string, got %T", raw))
		}
	}
	if raw, ok := section["salt"]; ok {
		str, isStr := raw.(string)
		if isStr {
			t.Salt = str
		} else {
			errs = append(errs, fmt.Errorf("[telemetry] salt must be a string, got %T", raw))
		}
	}
	if raw, ok := section["disclosed"]; ok {
		b, isBool := raw.(bool)
		if isBool {
			t.Disclosed = b
		} else {
			errs = append(errs, fmt.Errorf("[telemetry] disclosed must be a boolean, got %T (%v)", raw, raw))
		}
	}
	return t, errors.Join(errs...)
}

// WriteTelemetry replaces the [telemetry] section in s with t. The
// caller is responsible for s.Save() to persist.
//
// A zero TelemetrySection writes an empty section, which Store will
// then encode as a missing-section on Save (SetSection treats an empty
// map as "remove"). If you mean to clear the section explicitly, prefer
// ClearTelemetry - the intent is clearer at the call site.
func WriteTelemetry(s *userconfig.Store, t TelemetrySection) {
	section := map[string]any{}
	if t.Enabled != nil {
		section["enabled"] = *t.Enabled
	}
	if t.ID != "" {
		section["id"] = t.ID
	}
	if t.Salt != "" {
		section["salt"] = t.Salt
	}
	if t.Disclosed {
		section["disclosed"] = true
	}
	s.SetSection("telemetry", section)
}

// ClearTelemetry removes the [telemetry] section entirely. Use when
// the explicit intent is "no preference, no secrets, no record" - the
// CLI does not currently use this, but it makes the operation
// discoverable for future commands.
func ClearTelemetry(s *userconfig.Store) {
	s.SetSection("telemetry", nil)
}
