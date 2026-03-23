package gafferruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixture struct {
	Name     string          `json:"name"`
	Source   string          `json:"source"`
	Options  json.RawMessage `json:"options,omitempty"`
	SetState *struct {
		Partition *string `json:"partition"`
		State     string  `json:"state"`
	} `json:"setState,omitempty"`
	Events []json.RawMessage `json:"events,omitempty"`
	Expect fixtureExpect     `json:"expect"`
}

type fixtureExpect struct {
	Sources     map[string]json.RawMessage `json:"sources,omitempty"`
	State       json.RawMessage            `json:"state,omitempty"`
	States      map[string]json.RawMessage `json:"states,omitempty"`
	SharedState json.RawMessage            `json:"sharedState,omitempty"`
	Result      json.RawMessage            `json:"result,omitempty"`
	Emitted     []fixtureEmitted           `json:"emitted,omitempty"`
	Logs        []string                   `json:"logs,omitempty"`
	Error       *fixtureError              `json:"error,omitempty"`
	GetResult   *bool                      `json:"getResult,omitempty"`
}

type fixtureError struct {
	Code        string `json:"code"`
	Description string `json:"description,omitempty"`
}

type fixtureEmitted struct {
	StreamID  string `json:"streamId"`
	EventType string `json:"eventType"`
	Data      string `json:"data"`
}

func loadFixtures(t *testing.T, filename string) []fixture {
	t.Helper()
	path := filepath.Join("..", "..", "tools", "fixtures", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read fixtures: %v", err)
	}
	var fixtures []fixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatalf("failed to parse fixtures: %v", err)
	}
	return fixtures
}

func runFixtures(t *testing.T, filename string) {
	t.Helper()
	fixtures := loadFixtures(t, filename)
	for _, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			runFixture(t, f)
		})
	}
}

func runFixture(t *testing.T, f fixture) {
	t.Helper()

	var opts *string
	if f.Options != nil {
		s := string(f.Options)
		opts = &s
	}

	// Creation error (no events, no getResult)
	if f.Expect.Error != nil && len(f.Events) == 0 && (f.Expect.GetResult == nil || !*f.Expect.GetResult) {
		_, err := NewSession(f.Source, opts)
		if err == nil {
			t.Fatal("expected error on create")
		}
		assertFixtureError(t, err, f.Expect.Error)
		return
	}

	session, err := NewSession(f.Source, opts)
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}
	defer session.Destroy()

	// Check sources
	if f.Expect.Sources != nil {
		sourcesJSON := session.GetSources()
		if sourcesJSON == nil {
			t.Fatal("GetSources returned nil")
		}
		assertSourcesMatch(t, *sourcesJSON, f.Expect.Sources)
	}

	// Set state
	if f.SetState != nil {
		session.SetState(f.SetState.Partition, f.SetState.State)
	}

	// Feed events
	if len(f.Events) > 0 {
		var lastEmitted []struct{ streamID, eventType, data string }
		var lastLogs []string
		var lastFeedErr error

		session.OnEmit(func(streamID, eventType, data, _ string, _, _ bool) {
			lastEmitted = append(lastEmitted, struct{ streamID, eventType, data string }{streamID, eventType, data})
		})
		session.OnLog(func(message string) {
			lastLogs = append(lastLogs, message)
		})

		for _, evRaw := range f.Events {
			lastEmitted = nil
			lastLogs = nil
			if feedErr := session.Feed(string(evRaw)); feedErr != nil {
				lastFeedErr = feedErr
			}
		}

		// Check feed error (not getResult)
		if f.Expect.Error != nil && (f.Expect.GetResult == nil || !*f.Expect.GetResult) {
			if lastFeedErr == nil {
				t.Fatal("expected feed error")
			}
			assertFixtureError(t, lastFeedErr, f.Expect.Error)
			return
		}

		// Check emitted
		if f.Expect.Emitted != nil {
			if len(f.Expect.Emitted) != len(lastEmitted) {
				t.Fatalf("expected %d emitted events, got %d", len(f.Expect.Emitted), len(lastEmitted))
			}
			for i, exp := range f.Expect.Emitted {
				act := lastEmitted[i]
				if exp.StreamID != act.streamID {
					t.Fatalf("emitted[%d] streamId: expected %q, got %q", i, exp.StreamID, act.streamID)
				}
				if exp.EventType != act.eventType {
					t.Fatalf("emitted[%d] eventType: expected %q, got %q", i, exp.EventType, act.eventType)
				}
				if exp.Data != "" && exp.Data != act.data {
					t.Fatalf("emitted[%d] data: expected %q, got %q", i, exp.Data, act.data)
				}
			}
		}

		// Check logs
		if f.Expect.Logs != nil {
			if len(f.Expect.Logs) != len(lastLogs) {
				t.Fatalf("expected %d logs, got %d", len(f.Expect.Logs), len(lastLogs))
			}
			for i, exp := range f.Expect.Logs {
				if exp != lastLogs[i] {
					t.Fatalf("log[%d]: expected %q, got %q", i, exp, lastLogs[i])
				}
			}
		}
	}

	// Check getResult error
	if f.Expect.GetResult != nil && *f.Expect.GetResult && f.Expect.Error != nil {
		_, err := session.GetResult(nil)
		if err == nil {
			t.Fatal("expected getResult error")
		}
		assertFixtureError(t, err, f.Expect.Error)
		return
	}

	// Check state
	if f.Expect.State != nil {
		state := session.GetState(nil)
		if state == nil {
			t.Fatal("GetState returned nil")
		}
		assertJSONEqual(t, "state", string(f.Expect.State), *state)
	}

	// Check per-partition states
	for partition, expected := range f.Expect.States {
		p := partition
		state := session.GetState(&p)
		if string(expected) == "null" {
			if state != nil {
				t.Fatalf("expected nil state for partition %q, got %s", partition, *state)
			}
		} else {
			if state == nil {
				t.Fatalf("expected state for partition %q, got nil", partition)
			}
			assertJSONEqual(t, fmt.Sprintf("state[%s]", partition), string(expected), *state)
		}
	}

	// Check shared state
	if f.Expect.SharedState != nil {
		shared := session.GetSharedState()
		if shared == nil {
			t.Fatal("GetSharedState returned nil")
		}
		assertJSONEqual(t, "sharedState", string(f.Expect.SharedState), *shared)
	}

	// Check result
	if f.Expect.Result != nil {
		if string(f.Expect.Result) == "null" {
			result, err := session.GetResult(nil)
			if err != nil {
				t.Fatalf("GetResult error: %v", err)
			}
			if result != nil {
				t.Fatalf("expected nil result, got %s", *result)
			}
		} else {
			result, err := session.GetResult(nil)
			if err != nil {
				t.Fatalf("GetResult error: %v", err)
			}
			if result == nil {
				t.Fatal("GetResult returned nil")
			}
			assertJSONEqual(t, "result", string(f.Expect.Result), *result)
		}
	}
}

func assertFixtureError(t *testing.T, err error, expected *fixtureError) {
	t.Helper()
	var ge GafferError
	if !errors.As(err, &ge) {
		t.Fatalf("expected GafferError, got %T: %v", err, err)
	}
	if ge.ErrorCode() != expected.Code {
		t.Fatalf("expected code %q, got %q", expected.Code, ge.ErrorCode())
	}
	if expected.Description != "" && !strings.Contains(ge.ErrorDescription(), expected.Description) {
		t.Fatalf("expected description containing %q, got %q", expected.Description, ge.ErrorDescription())
	}
}

func assertJSONEqual(t *testing.T, label, expected, actual string) {
	t.Helper()
	var exp, act interface{}
	if err := json.Unmarshal([]byte(expected), &exp); err != nil {
		t.Fatalf("%s: invalid expected JSON: %v", label, err)
	}
	if err := json.Unmarshal([]byte(actual), &act); err != nil {
		t.Fatalf("%s: invalid actual JSON: %v", label, err)
	}
	expNorm, _ := json.Marshal(exp)
	actNorm, _ := json.Marshal(act)
	if string(expNorm) != string(actNorm) {
		t.Fatalf("%s:\n  expected: %s\n  actual:   %s", label, expNorm, actNorm)
	}
}

func assertSourcesMatch(t *testing.T, sourcesJSON string, expected map[string]json.RawMessage) {
	t.Helper()
	var sources map[string]json.RawMessage
	if err := json.Unmarshal([]byte(sourcesJSON), &sources); err != nil {
		t.Fatalf("failed to parse sources JSON: %v", err)
	}
	for key, expVal := range expected {
		actVal, ok := sources[key]
		if !ok {
			t.Fatalf("sources missing key %q", key)
		}
		expNorm, _ := json.Marshal(json.RawMessage(expVal))
		actNorm, _ := json.Marshal(json.RawMessage(actVal))
		if string(expNorm) != string(actNorm) {
			t.Fatalf("sources[%s]:\n  expected: %s\n  actual:   %s", key, expNorm, actNorm)
		}
	}
}

func TestFixtures_Sources(t *testing.T)    { runFixtures(t, "sources.json") }
func TestFixtures_State(t *testing.T)      { runFixtures(t, "state.json") }
func TestFixtures_Callbacks(t *testing.T)  { runFixtures(t, "callbacks.json") }
func TestFixtures_Errors(t *testing.T)     { runFixtures(t, "errors.json") }
func TestFixtures_Transforms(t *testing.T) { runFixtures(t, "transforms.json") }
func TestFixtures_Deletion(t *testing.T)   { runFixtures(t, "deletion.json") }
func TestFixtures_Versioning(t *testing.T) { runFixtures(t, "versioning.json") }
