package engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
}

func NewProjection(root string, cfg *config.Config, def *config.Projection, source string) *Projection {
	return &Projection{
		Root:          root,
		Config:        cfg,
		Def:           def,
		Source:        source,
		EngineVersion: cfg.EffectiveEngineVersion(def),
	}
}

func LoadProjection(name string) (*Projection, error) {
	root := project.FindRoot()
	if root == "" {
		return nil, project.ErrNotInProject
	}

	cfg, err := config.Load(filepath.Join(root, "gaffer.toml"))
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
	}, nil
}

func ReadSource(root, entry string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, entry))
	if err != nil {
		return "", fmt.Errorf("reading projection source: %w", err)
	}
	return string(data), nil
}

func CreateSession(proj *Projection, debug bool) (*gafferruntime.Session, gafferruntime.ProjectionInfo, error) {
	opts := buildSessionOptions(proj.Config, proj.Def, debug)
	session, err := gafferruntime.NewSession(proj.Source, opts)
	if err != nil {
		return nil, gafferruntime.ProjectionInfo{}, err
	}
	info := session.GetSources()
	return session, info, nil
}

func buildSessionOptions(cfg *config.Config, proj *config.Projection, debug bool) *string {
	opts := map[string]any{
		"engineVersion": cfg.EffectiveEngineVersion(proj),
	}

	if debug {
		opts["debug"] = true
	}

	if proj.ExecutionTimeout != nil && *proj.ExecutionTimeout > 0 {
		opts["executionTimeoutMs"] = *proj.ExecutionTimeout
	} else if cfg.ExecutionTimeout != nil && *cfg.ExecutionTimeout > 0 {
		opts["executionTimeoutMs"] = *cfg.ExecutionTimeout
	}

	if cfg.CompilationTimeout != nil && *cfg.CompilationTimeout > 0 {
		opts["compilationTimeoutMs"] = *cfg.CompilationTimeout
	}

	data, err := json.Marshal(opts)
	if err != nil {
		return nil
	}
	str := string(data)
	return &str
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
