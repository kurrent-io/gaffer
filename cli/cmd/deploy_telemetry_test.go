package cmd

import (
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry/telemetrytest"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

// emitDeployFamily runs a deploy-family command outside a project (so it fails
// fast at the config load, no server) and returns the single command_invoked's
// typed properties - enough to prove the emit is wired with the right variant.
func emitDeployFamily[T any](t *testing.T, args ...string) T {
	t.Helper()
	t.Chdir(t.TempDir())
	mock := telemetrytest.New()
	if err := runCmdWithTelemetry(t, mock, args...); err == nil {
		t.Fatalf("%v: expected an error outside a project", args)
	}
	envs := mock.Envelopes()
	if len(envs) != 1 {
		t.Fatalf("envelopes = %d, want 1", len(envs))
	}
	ci := testutil.MustType[telemetry.CommandInvoked](t, envs[0].Events[0])
	return testutil.MustType[T](t, ci.Properties)
}

func TestDeployFamily_EmitsCommandInvoked(t *testing.T) {
	// Outside a project every command classifies as manifest_not_found - a
	// consistent, network-free way to assert each command emits its own variant
	// with the right command name.
	t.Run("deploy", func(t *testing.T) {
		p := emitDeployFamily[telemetry.DeployCommandInvokedProperties](t, "deploy")
		assertCmd(t, p.Command, telemetry.CommandNameDeploy, p.Outcome)
	})
	t.Run("status", func(t *testing.T) {
		p := emitDeployFamily[telemetry.StatusCommandInvokedProperties](t, "status")
		assertCmd(t, p.Command, telemetry.CommandNameStatus, p.Outcome)
	})
	t.Run("diff", func(t *testing.T) {
		p := emitDeployFamily[telemetry.DiffCommandInvokedProperties](t, "diff", "orders")
		assertCmd(t, p.Command, telemetry.CommandNameDiff, p.Outcome)
	})
	t.Run("history", func(t *testing.T) {
		p := emitDeployFamily[telemetry.HistoryCommandInvokedProperties](t, "history", "orders")
		assertCmd(t, p.Command, telemetry.CommandNameHistory, p.Outcome)
		if p.RollbackApplied != nil {
			t.Errorf("RollbackApplied = %v, want nil (TUI never reached)", *p.RollbackApplied)
		}
	})
	t.Run("rollback", func(t *testing.T) {
		p := emitDeployFamily[telemetry.RollbackCommandInvokedProperties](t, "rollback", "orders", "23e1fa6")
		assertCmd(t, p.Command, telemetry.CommandNameRollback, p.Outcome)
		assertProdAbsent(t, p.ProdTarget)
	})
	t.Run("recreate", func(t *testing.T) {
		p := emitDeployFamily[telemetry.RecreateCommandInvokedProperties](t, "recreate", "orders")
		assertCmd(t, p.Command, telemetry.CommandNameRecreate, p.Outcome)
		assertProdAbsent(t, p.ProdTarget)
	})
	t.Run("enable", func(t *testing.T) {
		p := emitDeployFamily[telemetry.EnableCommandInvokedProperties](t, "enable", "orders")
		assertCmd(t, p.Command, telemetry.CommandNameEnable, p.Outcome)
		assertProdAbsent(t, p.ProdTarget)
	})
	t.Run("disable", func(t *testing.T) {
		p := emitDeployFamily[telemetry.DisableCommandInvokedProperties](t, "disable", "orders")
		assertCmd(t, p.Command, telemetry.CommandNameDisable, p.Outcome)
		assertProdAbsent(t, p.ProdTarget)
	})
	t.Run("delete", func(t *testing.T) {
		p := emitDeployFamily[telemetry.DeleteCommandInvokedProperties](t, "delete", "orders")
		assertCmd(t, p.Command, telemetry.CommandNameDelete, p.Outcome)
		assertProdAbsent(t, p.ProdTarget)
	})
}

// assertProdAbsent locks the family-wide rule that prod_target is absent when a
// mutating command fails before resolving the target (here, outside a project),
// with no absent-vs-false split across the commands.
func assertProdAbsent(t *testing.T, prod *bool) {
	t.Helper()
	if prod != nil {
		t.Errorf("ProdTarget = %v, want nil (target never resolved)", *prod)
	}
}

func TestDeploy_StampsFlagProps(t *testing.T) {
	// The flag props flow from opts into the event even when the run fails early,
	// so the wiring is proved without a server.
	t.Run("defaults", func(t *testing.T) {
		p := emitDeployFamily[telemetry.DeployCommandInvokedProperties](t, "deploy")
		assertBoolPtr(t, "DryRun", p.DryRun, false)
		assertBoolPtr(t, "NoValidate", p.NoValidate, false)
		if p.ProdTarget != nil {
			t.Errorf("ProdTarget = %v, want nil (server never reached)", *p.ProdTarget)
		}
	})
	t.Run("dry-run", func(t *testing.T) {
		p := emitDeployFamily[telemetry.DeployCommandInvokedProperties](t, "deploy", "--dry-run")
		assertBoolPtr(t, "DryRun", p.DryRun, true)
	})
	t.Run("no-validate", func(t *testing.T) {
		p := emitDeployFamily[telemetry.DeployCommandInvokedProperties](t, "deploy", "--no-validate")
		assertBoolPtr(t, "NoValidate", p.NoValidate, true)
	})
}

func TestRecreate_StampsNoValidate(t *testing.T) {
	p := emitDeployFamily[telemetry.RecreateCommandInvokedProperties](t, "recreate", "orders", "--no-validate")
	assertBoolPtr(t, "NoValidate", p.NoValidate, true)
}

func assertCmd(t *testing.T, got, want telemetry.CommandName, outcome telemetry.Outcome) {
	t.Helper()
	if got != want {
		t.Errorf("Command = %q, want %q", got, want)
	}
	if outcome != telemetry.OutcomeManifestNotFound {
		t.Errorf("Outcome = %q, want manifest_not_found", outcome)
	}
}

func assertBoolPtr(t *testing.T, name string, got *bool, want bool) {
	t.Helper()
	if got == nil {
		t.Fatalf("%s = nil, want %v", name, want)
	}
	if *got != want {
		t.Errorf("%s = %v, want %v", name, *got, want)
	}
}
