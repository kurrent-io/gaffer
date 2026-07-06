package mcpserver

import (
	"strings"
	"testing"
)

func TestOperateVerbsProjectless(t *testing.T) {
	s := newProjectlessServer(t)
	const want = "no gaffer project found"
	checks := []struct {
		name string
		msg  string
	}{
		{"deploy_pause", callToolExpectError(t, s.handleDeployPause, operateInput{Name: "x"})},
		{"deploy_resume", callToolExpectError(t, s.handleDeployResume, operateInput{Name: "x"})},
		{"deploy_abort", callToolExpectError(t, s.handleDeployAbort, operateInput{Name: "x"})},
		{"deploy_delete", callToolExpectError(t, s.handleDeployDelete, deployDeleteInput{Name: "x"})},
		{"deploy_recreate", callToolExpectError(t, s.handleDeployRecreate, deployRecreateInput{Name: "x"})},
		{"deploy_rollback", callToolExpectError(t, s.handleDeployRollback, deployRollbackInput{Name: "x", Hash: "abcd"})},
	}
	for _, c := range checks {
		if !strings.Contains(c.msg, want) {
			t.Errorf("%s: got %q, want substring %q", c.name, c.msg, want)
		}
	}
}

func TestOperateVerbsNameRequired(t *testing.T) {
	s := setupTestProjectWithEnv(t)
	if msg := callToolExpectError(t, s.handleDeployPause, operateInput{}); !strings.Contains(msg, "name required") {
		t.Errorf("got %q, want the name-required message", msg)
	}
}

// System projections run the database's own plumbing; every verb refuses
// them before any connection is attempted (the fixture's env would fail to
// dial, so reaching resolution would produce a different message).
func TestOperateVerbsRefuseSystemProjections(t *testing.T) {
	s := setupTestProject(t)
	checks := []struct {
		name string
		msg  string
	}{
		{"deploy_pause", callToolExpectError(t, s.handleDeployPause, operateInput{Name: "$by_category"})},
		{"deploy_delete", callToolExpectError(t, s.handleDeployDelete, deployDeleteInput{Name: "$by_category"})},
		{"deploy_recreate", callToolExpectError(t, s.handleDeployRecreate, deployRecreateInput{Name: "$by_category"})},
		{"deploy_rollback", callToolExpectError(t, s.handleDeployRollback, deployRollbackInput{Name: "$by_category", Hash: "abcd"})},
	}
	for _, c := range checks {
		if !strings.Contains(c.msg, "system projection") {
			t.Errorf("%s: got %q, want the system-projection refusal", c.name, c.msg)
		}
	}
}

func TestOperateVerbsUnknownEnv(t *testing.T) {
	s := setupTestProjectWithEnv(t)
	const want = `unknown environment "nope"`
	if msg := callToolExpectError(t, s.handleDeployAbort, operateInput{Name: "order-count", Env: "nope"}); !strings.Contains(msg, want) {
		t.Errorf("got %q, want substring %q", msg, want)
	}
}

// The hash is validated before env resolution or any connection - the
// fixture has no envs, so reaching resolution would fail differently.
func TestDeployRollbackBadHash(t *testing.T) {
	s := setupTestProject(t)
	for hash, want := range map[string]string{
		"abc":      "too short",
		"xyzk":     "not hexadecimal",
		"origin/x": "not hexadecimal",
	} {
		if msg := callToolExpectError(t, s.handleDeployRollback, deployRollbackInput{Name: "order-count", Hash: hash}); !strings.Contains(msg, want) {
			t.Errorf("hash %q: got %q, want %q", hash, msg, want)
		}
	}
}
