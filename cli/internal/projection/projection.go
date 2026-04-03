package projection

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

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

const ZeroUUID = "00000000-0000-0000-0000-000000000000"

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
			obj["eventId"] = ZeroUUID
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
