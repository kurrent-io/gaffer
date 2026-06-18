package remote

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
)

// State is a projection's lifecycle state, classified from the server's
// free-form status string into the three states commands care about.
type State string

const (
	// StateRunning: the projection is actively processing.
	StateRunning State = "running"
	// StateStopped: the projection is not running and not faulted - stopped,
	// completed, aborted, or in a transitional state (starting, loading).
	StateStopped State = "stopped"
	// StateFaulted: the projection stopped on an error; FaultReason explains it.
	StateFaulted State = "faulted"
	// StateUnknown: the server reported no status string.
	StateUnknown State = "unknown"
)

// Status is the live state of a deployed projection. It is gaffer's own shape,
// not the client's: commands depend on a classified State and named fields, not
// on parsing the server's status string themselves.
type Status struct {
	Name        string
	State       State
	Raw         string  // the server's raw status string, e.g. "Stopped/Faulted (Enabled)"
	FaultReason string  // set when State is StateFaulted
	Position    string  // last processed checkpoint position; empty if nothing processed
	Progress    float32 // percent processed; negative means unknown (-1) or unavailable (-2), else 0-100
	Mode        string  // e.g. Continuous
}

// classifyState mirrors how the server itself reads its status string (a
// substring test): the raw value can be a composite like "Stopped/Faulted
// (Enabled)", so order matters - faulted wins over running, running over the
// rest. An empty string means the server reported no status.
//
// An in-flight stop reads as e.g. "Stopping/Running" and so classifies as
// running; this matches the server's own telemetry, which counts the same
// composite as running.
func classifyState(raw string) State {
	switch {
	case raw == "":
		return StateUnknown
	case strings.Contains(raw, "Faulted"):
		return StateFaulted
	case strings.Contains(raw, "Running"):
		return StateRunning
	default:
		return StateStopped
	}
}

func toStatus(s kurrentdb.ProjectionStatus) Status {
	state := classifyState(s.Status)
	st := Status{
		Name:     s.Name,
		State:    state,
		Raw:      s.Status,
		Position: s.Position,
		Progress: s.Progress,
		Mode:     s.Mode,
	}
	if state == StateFaulted {
		st.FaultReason = s.StateReason
	}
	return st
}

// statuses fetches every continuous projection's live status, including the
// system ($-prefixed) ones. List filters those out; Status and Exists search
// the full set so a lookup by exact name still works.
func (c *Client) statuses(ctx context.Context) ([]Status, error) {
	raw, err := c.proj.ListContinuous(ctx, leaderOpts())
	if err != nil {
		return nil, classify(err)
	}
	out := make([]Status, 0, len(raw))
	for _, s := range raw {
		out = append(out, toStatus(s))
	}
	return out, nil
}

// List returns the live status of every user projection on the server,
// excluding the system ($-prefixed) projections gaffer never manages.
func (c *Client) List(ctx context.Context) ([]Status, error) {
	all, err := c.statuses(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Status, 0, len(all))
	for _, s := range all {
		if strings.HasPrefix(s.Name, "$") {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// Status returns the live status of a single projection by name, or ErrNotFound
// if no projection with that name is deployed.
//
// It searches the full statistics listing rather than the client's by-name
// GetStatus: that helper indexes the first result unconditionally and panics
// when the projection is absent (the server returns an empty result, not an
// error).
func (c *Client) Status(ctx context.Context, name string) (*Status, error) {
	all, err := c.statuses(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Name == name {
			return &all[i], nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
}

// Exists reports whether a projection with the given name is deployed.
func (c *Client) Exists(ctx context.Context, name string) (bool, error) {
	_, err := c.Status(ctx, name)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrNotFound):
		return false, nil
	default:
		return false, err
	}
}
