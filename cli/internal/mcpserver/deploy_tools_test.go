package mcpserver

import (
	"strings"
	"testing"
)

func TestDeployToolsProjectless(t *testing.T) {
	s := newProjectlessServer(t)
	const want = "no gaffer project found"
	checks := []struct {
		name string
		msg  string
	}{
		{"deploy_status", callToolExpectError(t, s.handleDeployStatus, deployStatusInput{})},
		{"deploy_plan", callToolExpectError(t, s.handleDeployPlan, deployPlanInput{})},
		{"deploy_history", callToolExpectError(t, s.handleDeployHistory, deployHistoryInput{Name: "x"})},
	}
	for _, c := range checks {
		if !strings.Contains(c.msg, want) {
			t.Errorf("%s: got %q, want substring %q", c.name, c.msg, want)
		}
	}
}

// An unknown env name is rejected at resolution, before any connection
// attempt, proving each tool's env arg is threaded to ResolveEnv rather
// than silently falling back to the default env.
func TestDeployToolsUnknownEnv(t *testing.T) {
	s := setupTestProjectWithEnv(t)
	const want = `unknown environment "nope"`
	checks := []struct {
		name string
		msg  string
	}{
		{"deploy_status", callToolExpectError(t, s.handleDeployStatus, deployStatusInput{Env: "nope"})},
		{"deploy_plan", callToolExpectError(t, s.handleDeployPlan, deployPlanInput{Env: "nope"})},
		{"deploy_history", callToolExpectError(t, s.handleDeployHistory, deployHistoryInput{Name: "order-count", Env: "nope"})},
	}
	for _, c := range checks {
		if !strings.Contains(c.msg, want) {
			t.Errorf("%s: got %q, want substring %q", c.name, c.msg, want)
		}
	}
}

// A project with no [env.*] blocks can't reach a server; the tools surface
// the config gap instead of dialling nothing.
func TestDeployToolsNoEnvConfigured(t *testing.T) {
	s := setupTestProject(t)
	msg := callToolExpectError(t, s.handleDeployStatus, deployStatusInput{})
	if !strings.Contains(msg, "no environments configured") {
		t.Errorf("got %q, want the no-environments message", msg)
	}
}

// The name is validated against gaffer.toml before env resolution or any
// connection - the fixture has no envs, so reaching resolution would fail
// with a different message.
func TestDeployPlanUnknownName(t *testing.T) {
	s := setupTestProject(t)
	msg := callToolExpectError(t, s.handleDeployPlan, deployPlanInput{Name: "nope"})
	if !strings.Contains(msg, `projection "nope" is not in gaffer.toml`) {
		t.Errorf("got %q, want the unknown-projection message", msg)
	}
}

func TestDeployHistoryNameRequired(t *testing.T) {
	s := setupTestProjectWithEnv(t)
	msg := callToolExpectError(t, s.handleDeployHistory, deployHistoryInput{})
	if !strings.Contains(msg, "name required") {
		t.Errorf("got %q, want the name-required message", msg)
	}
}
