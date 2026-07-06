package mcpserver

import (
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

func TestDeployApplyProjectless(t *testing.T) {
	s := newProjectlessServer(t)
	if msg := callToolExpectError(t, s.handleDeployApply, deployApplyInput{}); !strings.Contains(msg, "no gaffer project found") {
		t.Errorf("got %q, want the projectless gate", msg)
	}
}

func TestDeployApplyUnknownName(t *testing.T) {
	s := setupTestProject(t)
	if msg := callToolExpectError(t, s.handleDeployApply, deployApplyInput{Name: "nope"}); !strings.Contains(msg, `projection "nope" is not in gaffer.toml`) {
		t.Errorf("got %q, want the unknown-projection message", msg)
	}
}

// The preflight runs before any connection (the fixture has no envs, so
// reaching resolution would fail differently) and one uncompilable
// projection refuses the whole run.
func TestDeployApplyPreflightRefusesBeforeConnecting(t *testing.T) {
	p := testutil.NewProject(t).
		AddProjection("good", "fromAll().when({ $init() { return {}; } })").
		AddProjection("bad", "fromAll(.when({").
		Save()
	s := New(p.Dir, p.Cfg, "test")

	msg := callToolExpectError(t, s.handleDeployApply, deployApplyInput{Name: "bad"})
	if !strings.Contains(msg, "preflight failed, nothing was deployed") || !strings.Contains(msg, "bad:") {
		t.Errorf("got %q, want the preflight refusal naming the projection", msg)
	}

	// Deploying everything hits the same gate: the good projection doesn't
	// proceed while a sibling fails preflight.
	msg = callToolExpectError(t, s.handleDeployApply, deployApplyInput{})
	if !strings.Contains(msg, "preflight failed, nothing was deployed") {
		t.Errorf("got %q, want the all-or-nothing preflight refusal", msg)
	}
}

// A compiling projection passes preflight and reaches env resolution, which
// rejects an unknown env before any connection.
func TestDeployApplyUnknownEnv(t *testing.T) {
	s := setupTestProjectWithEnv(t)
	if msg := callToolExpectError(t, s.handleDeployApply, deployApplyInput{Name: "order-count", Env: "nope"}); !strings.Contains(msg, `unknown environment "nope"`) {
		t.Errorf("got %q, want the unknown-env message", msg)
	}
}
