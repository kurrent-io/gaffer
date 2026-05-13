package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

// setupTelemetryCmdTest carves a clean filesystem + env baseline for
// each `gaffer config telemetry ...` test. Returns the simulated
// home directory; cwd is set inside it so the workspace walk is
// bounded by HOME and can't observe stray gaffer.toml files outside
// the test sandbox.
func setupTelemetryCmdTest(t *testing.T) (home string) {
	t.Helper()

	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows equivalent; safe on POSIX

	configDir := filepath.Join(home, ".config", "gaffer")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GAFFER_CONFIG_DIR", configDir)

	cwd := filepath.Join(home, "dev", "myproject")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)

	// Real env vars in the test runner's environment must not leak
	// into the public CheckOptOut path. t.Setenv-empty then Unsetenv
	// gives us lookup-returns-false within the test, with cleanup
	// handled by t.Cleanup.
	for _, k := range []string{"GAFFER_TELEMETRY_OPTOUT", "KURRENTDB_TELEMETRY_OPTOUT", "DO_NOT_TRACK", "GAFFER_TELEMETRY_DEBUG"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}

	// Default to TTY=true so tests exercise the realistic interactive
	// path; tests that need the captured-stderr branch override.
	t.Cleanup(telemetry.IsTTYCheckForTesting(func(io.Writer) bool { return true }))

	return home
}

// runCmd invokes the root command with args and returns
// (stdout, stderr, err). Each test constructs a fresh root via
// NewRootCmd for isolation, per the package's existing convention.
func runCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCmd()
	var stdout, stderr bytes.Buffer
	root.SetArgs(args)
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	err := ExecuteRoot(context.Background(), root)
	return stdout.String(), stderr.String(), err
}

// ----- status -----

func TestConfigTelemetryStatus_AllDefault(t *testing.T) {
	setupTelemetryCmdTest(t)
	stdout, _, err := runCmd(t, "config", "telemetry", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{
		"id:         none\n",
		"telemetry:  enabled\n",
		"user:       unset",
		"env:        not set",
		"workspace:  not set",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("status output missing %q; got:\n%s", want, stdout)
		}
	}
}

func TestConfigTelemetryStatus_PersistedID(t *testing.T) {
	setupTelemetryCmdTest(t)
	dir, _ := userconfig.DefaultDir()
	store, _ := userconfig.Load(dir)
	id, _ := telemetry.MintIdentity()
	telemetry.StageIdentity(store, id)
	if err := store.Save(); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	stdout, _, err := runCmd(t, "config", "telemetry", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(stdout, "id:         "+id.TelemetryID) {
		t.Errorf("status output missing minted id; got:\n%s", stdout)
	}
}

func TestConfigTelemetryStatus_UserDisabled(t *testing.T) {
	setupTelemetryCmdTest(t)
	dir, _ := userconfig.DefaultDir()
	store, _ := userconfig.Load(dir)
	off := false
	telemetry.WriteTelemetry(store, telemetry.TelemetrySection{Enabled: &off})
	if err := store.Save(); err != nil {
		t.Fatalf("seed: %v", err)
	}

	stdout, _, err := runCmd(t, "config", "telemetry", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{
		"telemetry:  disabled",
		"user:       disabled",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q; got:\n%s", want, stdout)
		}
	}
}

func TestConfigTelemetryStatus_EnvDisabled(t *testing.T) {
	setupTelemetryCmdTest(t)
	t.Setenv("DO_NOT_TRACK", "1")

	stdout, _, err := runCmd(t, "config", "telemetry", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{
		"telemetry:  disabled",
		"env:        disabled (DO_NOT_TRACK)",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q; got:\n%s", want, stdout)
		}
	}
}

func TestConfigTelemetryStatus_WorkspaceDisabled(t *testing.T) {
	home := setupTelemetryCmdTest(t)
	cwd := filepath.Join(home, "dev", "myproject")
	gafferToml := filepath.Join(cwd, "gaffer.toml")
	if err := os.WriteFile(gafferToml, []byte("telemetry = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := runCmd(t, "config", "telemetry", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{
		"telemetry:  disabled",
		"workspace:  disabled (telemetry=false in " + gafferToml + ")",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q; got:\n%s", want, stdout)
		}
	}
}

func TestConfigTelemetryStatus_UserPartialErrorSurfaced(t *testing.T) {
	setupTelemetryCmdTest(t)
	dir, _ := userconfig.DefaultDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[telemetry]\nenabled = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := runCmd(t, "config", "telemetry", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(stdout, "user:       unset (error:") {
		t.Errorf("missing user error surface; got:\n%s", stdout)
	}
}

// ----- on -----

func TestConfigTelemetryOn_FreshMintsAndNotifies(t *testing.T) {
	setupTelemetryCmdTest(t)

	stdout, stderr, err := runCmd(t, "config", "telemetry", "on")
	if err != nil {
		t.Fatalf("on: %v", err)
	}
	if !strings.Contains(stdout, "Telemetry enabled.") {
		t.Errorf("stdout missing confirmation; got:\n%s", stdout)
	}
	// Notice on stderr.
	if !strings.Contains(stderr, "Gaffer collects usage data") {
		t.Errorf("stderr missing notice; got:\n%s", stderr)
	}

	// Identity persisted.
	dir, _ := userconfig.DefaultDir()
	store, _ := userconfig.Load(dir)
	id, ok, _ := telemetry.IdentityFromConfig(store)
	if !ok {
		t.Error("identity not persisted after on")
	}
	if id.IsZero() {
		t.Error("identity zero")
	}
	// Enabled=true persisted.
	got, _ := telemetry.LoadTelemetry(store)
	if got.Enabled == nil || *got.Enabled != true {
		t.Errorf("Enabled = %v, want &true", got.Enabled)
	}
}

func TestConfigTelemetryOn_EnvOptOutBlocks(t *testing.T) {
	setupTelemetryCmdTest(t)
	t.Setenv("DO_NOT_TRACK", "1")

	stdout, stderr, err := runCmd(t, "config", "telemetry", "on")
	if err != nil {
		t.Fatalf("on: %v", err)
	}
	for _, want := range []string{
		"Telemetry enabled in user config.",
		"still disabled by:",
		"DO_NOT_TRACK",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q; got:\n%s", want, stdout)
		}
	}
	// No notice when blocked by env.
	if strings.Contains(stderr, "Gaffer collects usage data") {
		t.Errorf("notice written despite env opt-out; stderr:\n%s", stderr)
	}
	// And no identity minted.
	dir, _ := userconfig.DefaultDir()
	store, _ := userconfig.Load(dir)
	if _, ok, _ := telemetry.IdentityFromConfig(store); ok {
		t.Error("identity minted despite env opt-out")
	}
}

func TestConfigTelemetryOn_ExistingIdentitySkipsNotice(t *testing.T) {
	setupTelemetryCmdTest(t)
	dir, _ := userconfig.DefaultDir()
	store, _ := userconfig.Load(dir)
	prev, _ := telemetry.MintIdentity()
	telemetry.StageIdentity(store, prev)
	if err := store.Save(); err != nil {
		t.Fatalf("seed: %v", err)
	}

	stdout, stderr, err := runCmd(t, "config", "telemetry", "on")
	if err != nil {
		t.Fatalf("on: %v", err)
	}
	if !strings.Contains(stdout, "Telemetry enabled.") {
		t.Errorf("stdout missing confirmation; got:\n%s", stdout)
	}
	if strings.Contains(stderr, "Gaffer collects usage data") {
		t.Errorf("notice written for existing identity; stderr:\n%s", stderr)
	}
	// Identity preserved.
	store2, _ := userconfig.Load(dir)
	got, _, _ := telemetry.IdentityFromConfig(store2)
	if got.TelemetryID != prev.TelemetryID {
		t.Errorf("identity changed: got %s, want %s", got.TelemetryID, prev.TelemetryID)
	}
}

// ----- off -----

func TestConfigTelemetryOff_PrintsClearedIDForRTBF(t *testing.T) {
	setupTelemetryCmdTest(t)
	dir, _ := userconfig.DefaultDir()
	store, _ := userconfig.Load(dir)
	prev, _ := telemetry.MintIdentity()
	on := true
	telemetry.WriteTelemetry(store, telemetry.TelemetrySection{Enabled: &on, ID: prev.TelemetryID, Salt: prev.Salt})
	if err := store.Save(); err != nil {
		t.Fatalf("seed: %v", err)
	}

	stdout, _, err := runCmd(t, "config", "telemetry", "off")
	if err != nil {
		t.Fatalf("off: %v", err)
	}
	for _, want := range []string{
		"Telemetry disabled.",
		prev.TelemetryID,
		"privacy@kurrent.io",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q; got:\n%s", want, stdout)
		}
	}

	// Identity cleared, Enabled=false persisted.
	store2, _ := userconfig.Load(dir)
	if _, ok, _ := telemetry.IdentityFromConfig(store2); ok {
		t.Error("identity not cleared after off")
	}
	got, _ := telemetry.LoadTelemetry(store2)
	if got.Enabled == nil || *got.Enabled != false {
		t.Errorf("Enabled = %v, want &false", got.Enabled)
	}
}

func TestConfigTelemetryOff_NoExistingIDStillSucceeds(t *testing.T) {
	setupTelemetryCmdTest(t)

	stdout, _, err := runCmd(t, "config", "telemetry", "off")
	if err != nil {
		t.Fatalf("off: %v", err)
	}
	if !strings.Contains(stdout, "Telemetry disabled.") {
		t.Errorf("stdout missing confirmation; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "privacy@kurrent.io") {
		t.Errorf("RTBF line shown despite no prior id; got:\n%s", stdout)
	}

	// Enabled=false persisted even with no prior id.
	dir, _ := userconfig.DefaultDir()
	store, _ := userconfig.Load(dir)
	got, _ := telemetry.LoadTelemetry(store)
	if got.Enabled == nil || *got.Enabled != false {
		t.Errorf("Enabled = %v, want &false", got.Enabled)
	}
}

func TestConfigTelemetryOn_OnThenOnNoDoubleNotice(t *testing.T) {
	setupTelemetryCmdTest(t)

	_, stderr1, err := runCmd(t, "config", "telemetry", "on")
	if err != nil {
		t.Fatalf("first on: %v", err)
	}
	if !strings.Contains(stderr1, "Gaffer collects usage data") {
		t.Fatalf("first on missing notice; stderr:\n%s", stderr1)
	}

	// Capture the minted id so we can confirm it survives the second on.
	dir, _ := userconfig.DefaultDir()
	store, _ := userconfig.Load(dir)
	idAfterFirst, _, _ := telemetry.IdentityFromConfig(store)

	_, stderr2, err := runCmd(t, "config", "telemetry", "on")
	if err != nil {
		t.Fatalf("second on: %v", err)
	}
	if strings.Contains(stderr2, "Gaffer collects usage data") {
		t.Errorf("second on re-printed notice; stderr:\n%s", stderr2)
	}

	// Identity preserved across the second on (no re-mint).
	store2, _ := userconfig.Load(dir)
	idAfterSecond, _, _ := telemetry.IdentityFromConfig(store2)
	if idAfterFirst.TelemetryID != idAfterSecond.TelemetryID {
		t.Errorf("id changed across on/on: %s -> %s", idAfterFirst.TelemetryID, idAfterSecond.TelemetryID)
	}
}

func TestConfigTelemetryOn_CorruptConfigWarnsAndProceeds(t *testing.T) {
	setupTelemetryCmdTest(t)
	dir, _ := userconfig.DefaultDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[telemetry]\nenabled = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runCmd(t, "config", "telemetry", "on")
	if err != nil {
		t.Fatalf("on with corrupt config: %v", err)
	}
	if !strings.Contains(stderr, "prior telemetry config unreadable") {
		t.Errorf("missing warning on stderr; got:\n%s", stderr)
	}
	if !strings.Contains(stdout, "Telemetry enabled.") {
		t.Errorf("on didn't proceed past parse error; stdout:\n%s", stdout)
	}
	// Identity minted despite parse error - recovery worked.
	store, _ := userconfig.Load(dir)
	if _, ok, _ := telemetry.IdentityFromConfig(store); !ok {
		t.Error("identity not minted after corrupt-config recovery")
	}
}

func TestConfigTelemetryOff_CorruptConfigSkipsRTBFLine(t *testing.T) {
	setupTelemetryCmdTest(t)
	dir, _ := userconfig.DefaultDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[telemetry]\nenabled = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, _, err := runCmd(t, "config", "telemetry", "off")
	if err != nil {
		t.Fatalf("off with corrupt config: %v", err)
	}
	if !strings.Contains(stdout, "Telemetry disabled.") {
		t.Errorf("off didn't confirm; stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "couldn't be recovered") {
		t.Errorf("missing unrecoverable-id message; stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "privacy@kurrent.io") {
		t.Errorf("missing privacy contact; stdout:\n%s", stdout)
	}
}

func TestConfigTelemetry_OnThenOffRoundTrip(t *testing.T) {
	setupTelemetryCmdTest(t)

	_, _, err := runCmd(t, "config", "telemetry", "on")
	if err != nil {
		t.Fatalf("on: %v", err)
	}
	stdout, _, err := runCmd(t, "config", "telemetry", "off")
	if err != nil {
		t.Fatalf("off: %v", err)
	}
	if !strings.Contains(stdout, "privacy@kurrent.io") {
		t.Errorf("RTBF line missing after on+off; got:\n%s", stdout)
	}

	// Status now shows: id none, telemetry disabled, user disabled.
	stdout, _, err = runCmd(t, "config", "telemetry", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{
		"id:         none",
		"telemetry:  disabled",
		"user:       disabled",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("post-cycle status missing %q; got:\n%s", want, stdout)
		}
	}
}
