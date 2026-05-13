package telemetry

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

// startupTest seeds a clean baseline: a real store in t.TempDir,
// a cwd inside a fake home with no gaffer.toml, and all opt-out
// env vars cleared so the test controls them explicitly.
func startupTest(t *testing.T) (store *userconfig.Store, cwd, home string) {
	t.Helper()

	home = t.TempDir()
	cwd = filepath.Join(home, "proj")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := userconfig.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"GAFFER_TELEMETRY_OPTOUT", "KURRENTDB_TELEMETRY_OPTOUT", "DO_NOT_TRACK", "GAFFER_TELEMETRY_DEBUG"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
	pretendTTY(t)
	return store, cwd, home
}

func TestStartupGate_OptedOutByUserReturnsNil(t *testing.T) {
	store, cwd, home := startupTest(t)
	off := false
	WriteTelemetry(store, TelemetrySection{Enabled: &off})
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	var notice bytes.Buffer
	c := StartupGate(store, cwd, home, "", &notice, Invocation{})
	if c != nil {
		t.Errorf("StartupGate returned %v, want nil for user-disabled", c)
	}
	if notice.Len() != 0 {
		t.Errorf("notice written despite opt-out: %q", notice.String())
	}
}

func TestStartupGate_OptedOutByEnvReturnsNil(t *testing.T) {
	store, cwd, home := startupTest(t)
	t.Setenv("DO_NOT_TRACK", "1")

	var notice bytes.Buffer
	c := StartupGate(store, cwd, home, "", &notice, Invocation{})
	if c != nil {
		t.Errorf("StartupGate returned %v, want nil for env-disabled", c)
	}
	if notice.Len() != 0 {
		t.Errorf("notice written despite env opt-out: %q", notice.String())
	}
}

func TestStartupGate_OptedOutByWorkspaceReturnsNil(t *testing.T) {
	store, cwd, home := startupTest(t)
	if err := os.WriteFile(filepath.Join(cwd, "gaffer.toml"), []byte("telemetry = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var notice bytes.Buffer
	c := StartupGate(store, cwd, home, "", &notice, Invocation{})
	if c != nil {
		t.Errorf("StartupGate returned %v, want nil for workspace-disabled", c)
	}
	if notice.Len() != 0 {
		t.Errorf("notice written despite workspace opt-out: %q", notice.String())
	}
}

func TestStartupGate_FreshInstallMintsAndNotifies(t *testing.T) {
	store, cwd, home := startupTest(t)

	var notice bytes.Buffer
	c := StartupGate(store, cwd, home, "", &notice, Invocation{})
	if c == nil {
		t.Fatal("StartupGate returned nil on fresh install")
	}
	if c.identity.IsZero() {
		t.Errorf("Client identity is zero after StartupGate")
	}
	if !strings.Contains(notice.String(), "Gaffer collects usage data") {
		t.Errorf("notice not written on fresh mint; got: %q", notice.String())
	}
}

func TestStartupGate_ExistingIdentitySkipsNotice(t *testing.T) {
	store, cwd, home := startupTest(t)
	seed, _ := MintIdentity()
	StageIdentity(store, seed)
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	var notice bytes.Buffer
	c := StartupGate(store, cwd, home, "", &notice, Invocation{})
	if c == nil {
		t.Fatal("StartupGate returned nil for existing-identity case")
	}
	if c.identity.TelemetryID != seed.TelemetryID {
		t.Errorf("identity = %s, want seeded %s", c.identity.TelemetryID, seed.TelemetryID)
	}
	if notice.Len() != 0 {
		t.Errorf("notice re-printed on existing identity: %q", notice.String())
	}
}

func TestStartupGate_MintFailureSurfacesWarning(t *testing.T) {
	// Read-only parent so Save fails inside MintAndPersist.
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })

	store, err := userconfig.Load(filepath.Join(parent, "child"))
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	for _, k := range []string{"GAFFER_TELEMETRY_OPTOUT", "KURRENTDB_TELEMETRY_OPTOUT", "DO_NOT_TRACK", "GAFFER_TELEMETRY_DEBUG"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}

	var notice bytes.Buffer
	c := StartupGate(store, cwd, t.TempDir(), "", &notice, Invocation{})
	if c != nil {
		t.Errorf("StartupGate returned %v, want nil on mint failure", c)
	}
	if !strings.Contains(notice.String(), "telemetry identity unavailable") {
		t.Errorf("missing mint-failure warning; got: %q", notice.String())
	}
}

func TestStartupGate_HashesProjectRoot(t *testing.T) {
	store, cwd, home := startupTest(t)

	c := StartupGate(store, cwd, home, cwd, &bytes.Buffer{}, Invocation{})
	if c == nil {
		t.Fatal("StartupGate returned nil despite mint succeeding")
	}
	if c.projectID == "" {
		t.Error("projectID is empty; expected hash of supplied projectRoot")
	}
	want := ProjectID(c.identity.Salt, cwd)
	if c.projectID != want {
		t.Errorf("projectID = %q, want %q (from ProjectID(salt, root))", c.projectID, want)
	}
}

func TestStartupGate_LeavesProjectIDEmptyWhenRootEmpty(t *testing.T) {
	store, cwd, home := startupTest(t)

	c := StartupGate(store, cwd, home, "", &bytes.Buffer{}, Invocation{})
	if c == nil {
		t.Fatal("StartupGate returned nil despite mint succeeding")
	}
	if c.projectID != "" {
		t.Errorf("projectID = %q, want empty when projectRoot is empty", c.projectID)
	}
}

func TestStartupGate_NormalizesProjectRootBeforeHashing(t *testing.T) {
	// ProjectID's contract is "cleaned, absolute path". StartupGate
	// honours it regardless of how main.go obtained the root, so an
	// unclean variant (trailing slash, redundant ./) hashes to the
	// same project_id as the clean form.
	store, cwd, home := startupTest(t)

	clean := StartupGate(store, cwd, home, cwd, &bytes.Buffer{}, Invocation{})
	if clean == nil {
		t.Fatal("StartupGate returned nil for clean root")
	}
	unclean := StartupGate(store, cwd, home, cwd+"/./", &bytes.Buffer{}, Invocation{})
	if unclean == nil {
		t.Fatal("StartupGate returned nil for unclean root")
	}
	if clean.projectID != unclean.projectID {
		t.Errorf("projectID differs after normalisation: clean=%q unclean=%q", clean.projectID, unclean.projectID)
	}
}

func TestStartupGate_SameRootProducesSameID(t *testing.T) {
	// User-visible invariant: two CLI processes launched from
	// different subdirectories of the same project must stamp the
	// same project_id. Hash input is the resolved root, not the cwd,
	// so this holds as long as main.go feeds the resolved root.
	store, cwd, home := startupTest(t)
	c1 := StartupGate(store, cwd, home, cwd, &bytes.Buffer{}, Invocation{})
	if c1 == nil {
		t.Fatal("first StartupGate returned nil")
	}
	c2 := StartupGate(store, cwd, home, cwd, &bytes.Buffer{}, Invocation{})
	if c2 == nil {
		t.Fatal("second StartupGate returned nil")
	}
	if c1.projectID != c2.projectID {
		t.Errorf("projectID drifted across calls: %q != %q", c1.projectID, c2.projectID)
	}
}

func TestStartupGate_InvokerIDSuppressesNotice(t *testing.T) {
	// A spawner identifying itself via --invoker-id is expected to
	// have shown its own disclosure (the VS Code extension surfaces
	// a first-activation notification). Re-printing the CLI banner
	// inside the spawn would be invisible or duplicative.
	store, cwd, home := startupTest(t)

	var notice bytes.Buffer
	inv := Invocation{InvokerID: "11111111-1111-4111-8111-111111111111"}
	c := StartupGate(store, cwd, home, "", &notice, inv)
	if c == nil {
		t.Fatal("StartupGate returned nil despite mint succeeding")
	}
	if notice.Len() != 0 {
		t.Errorf("notice written despite --invoker-id: %q", notice.String())
	}
	if c.invocation.InvokerID != inv.InvokerID {
		t.Errorf("Client.invocation.InvokerID = %q, want %q", c.invocation.InvokerID, inv.InvokerID)
	}
}

func TestStartupGate_PreSetDisclosedFlagSuppressesNotice(t *testing.T) {
	// Disclosed=true persists across `config telemetry off` -> `on`
	// round-trips. A subsequent mint must not re-show the banner.
	store, cwd, home := startupTest(t)
	WriteTelemetry(store, TelemetrySection{Disclosed: true})
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	var notice bytes.Buffer
	c := StartupGate(store, cwd, home, "", &notice, Invocation{})
	if c == nil {
		t.Fatal("StartupGate returned nil despite mint succeeding")
	}
	if notice.Len() != 0 {
		t.Errorf("notice written despite pre-set disclosed=true: %q", notice.String())
	}
}

func TestStartupGate_AppliesExtraOptions(t *testing.T) {
	store, cwd, home := startupTest(t)
	custom := "gaffer-cli/test-ua"

	c := StartupGate(store, cwd, home, "", &bytes.Buffer{}, Invocation{}, WithUserAgent(custom))
	if c == nil {
		t.Fatal("StartupGate returned nil")
	}
	if c.userAgent != custom {
		t.Errorf("userAgent = %q, want %q", c.userAgent, custom)
	}
}
