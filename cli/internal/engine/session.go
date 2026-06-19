package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/project"
)

type Projection struct {
	Root          string
	Config        *config.Config
	Def           *config.Projection
	Source        string
	EngineVersion int
	// QuirksVersion is the resolved target KurrentDB version (env > projection
	// > config). Empty string means unversioned.
	QuirksVersion string
}

func NewProjection(root string, cfg *config.Config, def *config.Projection, source string) *Projection {
	return &Projection{
		Root:          root,
		Config:        cfg,
		Def:           def,
		Source:        source,
		EngineVersion: cfg.EffectiveEngineVersion(def),
		QuirksVersion: cfg.EffectiveQuirksVersion(def),
	}
}

func LoadProjection(name string) (*Projection, error) {
	root := project.FindRoot()
	if root == "" {
		return nil, project.ErrNotInProject
	}

	cfg, err := config.Load(project.ConfigPath(root))
	if err != nil {
		return nil, err
	}

	proj := cfg.FindProjection(name)
	if proj == nil {
		return nil, fmt.Errorf("projection %q not found in gaffer.toml", name)
	}

	source, err := ReadSource(root, proj.Entry)
	if err != nil {
		return nil, err
	}

	return &Projection{
		Root:          root,
		Config:        cfg,
		Def:           proj,
		Source:        source,
		EngineVersion: cfg.EffectiveEngineVersion(proj),
		QuirksVersion: cfg.EffectiveQuirksVersion(proj),
	}, nil
}

func ReadSource(root, entry string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, entry))
	if err != nil {
		return "", fmt.Errorf("reading projection source: %w", err)
	}
	return string(data), nil
}

// CreateSession compiles the projection and returns a live session
// plus the populated ProjectionInfo. includeShape gates whether the
// runtime walks the AST a second time for projection_shape
// telemetry; long-running telemetry-active paths (dev / mcp / lsp
// when the Client is present on ctx) set it true, everything else
// false so they pay zero walker cost.
func CreateSession(proj *Projection, debug, includeShape bool) (*gafferruntime.Session, gafferruntime.ProjectionInfo, error) {
	opts := buildSessionOptions(proj, debug, includeShape)
	session, err := gafferruntime.NewSession(proj.Source, opts)
	if err != nil {
		return nil, gafferruntime.ProjectionInfo{}, err
	}
	info := session.GetSources()
	return session, info, nil
}

// buildSessionOptions reads the resolved engine/db versions off the
// Projection (already computed at construction). Re-resolving via
// cfg.EffectiveQuirksVersion here would risk diverging from the cached value
// if GAFFER_QUIRKS_VERSION changed between Projection construction and this
// call.
func buildSessionOptions(proj *Projection, debug, includeShape bool) *string {
	opts := map[string]any{
		"engineVersion": proj.EngineVersion,
	}

	if proj.QuirksVersion != "" {
		opts["quirksVersion"] = proj.QuirksVersion
	}

	if debug {
		opts["debug"] = true
	}

	if includeShape {
		opts["includeShape"] = true
	}

	// The [database_config] timeouts (and a per-projection execution_timeout)
	// declare the engine config expected on the deployment target; they are NOT
	// applied to local runs, because a wall-clock budget measured on a dev
	// machine says nothing about how the same handler behaves on the server.
	// gaffer's local engine instead uses a generous hang-guard so a runaway
	// projection can't wedge the process - the runtime's built-in default,
	// raised via GAFFER_TIMEOUT_MS for slow hardware.
	if ms := handlerTimeoutMs(); ms > 0 {
		opts["compilationTimeoutMs"] = ms
		opts["executionTimeoutMs"] = ms
	}

	// max_state_size is portable - serialized bytes are the same on a laptop and
	// the server - so unlike the timeouts it IS enforced locally, reproducing
	// the server's cap to catch state bloat before deploy.
	if db := proj.Config.DatabaseConfig; db != nil && db.MaxStateSize != nil && *db.MaxStateSize > 0 {
		opts["maxStateSizeBytes"] = *db.MaxStateSize
	}

	data, err := json.Marshal(opts)
	if err != nil {
		return nil
	}
	str := string(data)
	return &str
}

// EnvTimeoutMs is the environment variable that overrides gaffer's local
// compilation/execution hang-guard, in milliseconds. It bounds how long a
// projection may run locally before gaffer treats it as wedged; it is not a
// server-fidelity check (the [database_config] timeouts declare the server's,
// and are not applied locally).
const EnvTimeoutMs = "GAFFER_TIMEOUT_MS"

// handlerTimeoutMs returns the local hang-guard timeout in milliseconds from
// EnvTimeoutMs, applied to both compilation and execution. Returns 0, meaning
// "let the runtime apply its built-in default", when unset, non-positive, or
// unparseable. Parsed as a 32-bit value because the runtime reads these
// timeouts as int32; an out-of-range value (over ~24 days) is rejected rather
// than overflowing on the runtime side.
func handlerTimeoutMs() int {
	v := os.Getenv(EnvTimeoutMs)
	if v == "" {
		return 0
	}
	ms, err := strconv.ParseInt(v, 10, 32)
	if err != nil || ms <= 0 {
		return 0
	}
	return int(ms)
}

const zeroUUID = "00000000-0000-0000-0000-000000000000"

func LoadEvents(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading events file: %w", err)
	}

	var events []json.RawMessage
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, fmt.Errorf("parsing events file (expected JSON array): %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result := make([]string, len(events))
	for i, evt := range events {
		// UseNumber preserves JSON number precision through the round-trip.
		// Without it, large integers (e.g. $tb=long.MaxValue in soft-delete
		// $metadata events) would be coerced to float64 and re-marshaled as
		// approximations, breaking equality checks downstream.
		var obj map[string]any
		dec := json.NewDecoder(bytes.NewReader(evt))
		dec.UseNumber()
		if err := dec.Decode(&obj); err != nil {
			return nil, fmt.Errorf("event %d: %w", i+1, err)
		}

		if _, ok := obj["sequenceNumber"]; !ok {
			obj["sequenceNumber"] = i
		}
		if _, ok := obj["isJson"]; !ok {
			obj["isJson"] = true
		}
		if _, ok := obj["eventId"]; !ok {
			obj["eventId"] = zeroUUID
		}
		if _, ok := obj["created"]; !ok {
			obj["created"] = now
		}

		normalized, err := json.Marshal(obj)
		if err != nil {
			return nil, fmt.Errorf("event %d: %w", i+1, err)
		}
		result[i] = string(normalized)
	}

	return result, nil
}
