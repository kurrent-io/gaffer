package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/project"
)

type LoadedProjection struct {
	Root   string
	Config *config.Config
	Proj   *config.Projection
	Source string
	Engine string
}

func NewLoadedProjection(root string, cfg *config.Config, proj *config.Projection, source string) *LoadedProjection {
	return &LoadedProjection{
		Root:   root,
		Config: cfg,
		Proj:   proj,
		Source: source,
		Engine: proj.EffectiveEngine(),
	}
}

func LoadProjection(name string) (*LoadedProjection, error) {
	root := project.FindRoot()
	if root == "" {
		return nil, fmt.Errorf("not in a gaffer project (no gaffer.toml found)")
	}

	cfg, err := config.Load(filepath.Join(root, "gaffer.toml"))
	if err != nil {
		return nil, err
	}

	proj := cfg.FindProjection(name)
	if proj == nil {
		return nil, fmt.Errorf("projection %q not found in gaffer.toml", name)
	}

	source, err := os.ReadFile(filepath.Join(root, proj.Entry))
	if err != nil {
		return nil, fmt.Errorf("reading projection source: %w", err)
	}

	return &LoadedProjection{
		Root:   root,
		Config: cfg,
		Proj:   proj,
		Source: string(source),
		Engine: proj.EffectiveEngine(),
	}, nil
}

func NewSession(lp *LoadedProjection, debug bool) (*gafferruntime.Session, gafferruntime.QuerySources, error) {
	opts := BuildSessionOptions(lp.Config, lp.Proj, debug)
	session, err := gafferruntime.NewSession(lp.Source, opts)
	if err != nil {
		return nil, gafferruntime.QuerySources{}, err
	}
	info := session.GetSources()
	return session, info, nil
}

func BuildSessionOptions(cfg *config.Config, proj *config.Projection, debug bool) *string {
	opts := map[string]any{}

	if debug {
		opts["debug"] = true
	}

	if proj.Engine != "" {
		opts["version"] = proj.Engine
	}

	if proj.ExecutionTimeout != nil && *proj.ExecutionTimeout > 0 {
		opts["executionTimeoutMs"] = *proj.ExecutionTimeout
	} else if cfg.ExecutionTimeout != nil && *cfg.ExecutionTimeout > 0 {
		opts["executionTimeoutMs"] = *cfg.ExecutionTimeout
	}

	if cfg.CompilationTimeout != nil && *cfg.CompilationTimeout > 0 {
		opts["compilationTimeoutMs"] = *cfg.CompilationTimeout
	}

	if len(opts) == 0 {
		return nil
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
		var obj map[string]any
		if err := json.Unmarshal(evt, &obj); err != nil {
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
