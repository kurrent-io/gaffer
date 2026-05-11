package telemetry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

// emptyEnv is a lookup that pretends no env vars are set. Used by tests
// that need a clean baseline.
func emptyEnv(string) (string, bool) { return "", false }

// fixedEnv builds a lookup that returns the configured values; unset
// keys return (_, false).
func fixedEnv(kv map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := kv[k]
		return v, ok
	}
}

func TestIsTruthy(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"1", true}, {"true", true}, {"TRUE", true}, {"yes", true}, {"YES", true},
		{"on", true}, {"ON", true}, {" 1 ", true}, {"True", true},
		{"0", false}, {"false", false}, {"no", false}, {"off", false}, {"", false},
		{"random", false}, {"2", false},
	} {
		t.Run(tc.in, func(t *testing.T) {
			if got := isTruthy(tc.in); got != tc.want {
				t.Errorf("isTruthy(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestLayerState_String(t *testing.T) {
	for _, tc := range []struct {
		s    LayerState
		want string
	}{
		{LayerUnset, "unset"},
		{LayerEnabled, "enabled"},
		{LayerDisabled, "disabled"},
	} {
		if got := tc.s.String(); got != tc.want {
			t.Errorf("%d.String() = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestCheckOptOut_AllUnset(t *testing.T) {
	dir := t.TempDir()
	store, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r := checkOptOutWithEnv(store, t.TempDir(), os.TempDir(), emptyEnv)
	if r.Effective() != LayerUnset {
		t.Errorf("Effective = %v, want LayerUnset", r.Effective())
	}
	if r.IsDisabled() {
		t.Error("IsDisabled = true, want false")
	}
}

func TestCheckOptOut_UserDisabled(t *testing.T) {
	dir := t.TempDir()
	store, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	off := false
	WriteTelemetry(store, TelemetrySection{Enabled: &off})

	r := checkOptOutWithEnv(store, t.TempDir(), os.TempDir(), emptyEnv)
	if r.User.State != LayerDisabled {
		t.Errorf("User.State = %v, want LayerDisabled", r.User.State)
	}
	if r.User.Source != "user-config" {
		t.Errorf("User.Source = %q, want \"user-config\"", r.User.Source)
	}
	if r.User.Path == "" {
		t.Error("User.Path empty; want user config file path when State!=Unset")
	}
	if r.User.Value != "false" {
		t.Errorf("User.Value = %q, want \"false\"", r.User.Value)
	}
	if !r.IsDisabled() {
		t.Error("IsDisabled = false, want true")
	}
}

func TestCheckOptOut_UserUnsetHasNoPath(t *testing.T) {
	// When the user config exists but has no [telemetry] section,
	// User.Path stays empty - "Path only when this layer carries a
	// decision or err" rule.
	dir := t.TempDir()
	store, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r := checkOptOutWithEnv(store, t.TempDir(), os.TempDir(), emptyEnv)
	if r.User.State != LayerUnset {
		t.Fatalf("User.State = %v, want LayerUnset", r.User.State)
	}
	if r.User.Path != "" {
		t.Errorf("User.Path = %q, want empty when section absent", r.User.Path)
	}
	if r.User.Source != "user-config" {
		t.Errorf("User.Source = %q, want \"user-config\" (constant per kind, always populated)", r.User.Source)
	}
}

func TestCheckOptOut_UserParseErrorFailsOpen(t *testing.T) {
	// Malformed user config: type mismatch on enabled. The user layer
	// stays unset (fail-open), with the parse error on Layer.Err for
	// the status command to surface.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[telemetry]\nenabled = 1\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	store, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r := checkOptOutWithEnv(store, t.TempDir(), os.TempDir(), emptyEnv)
	if r.User.State != LayerUnset {
		t.Errorf("User.State = %v, want LayerUnset (fail-open on parse error)", r.User.State)
	}
	if r.User.Err == nil {
		t.Error("User.Err = nil, want surfaced parse error")
	}
}

func TestCheckOptOut_EnvDisabled(t *testing.T) {
	store, _ := userconfig.Load(t.TempDir())
	r := checkOptOutWithEnv(store, t.TempDir(), os.TempDir(), fixedEnv(map[string]string{
		"DO_NOT_TRACK": "1",
	}))
	if r.Env.State != LayerDisabled {
		t.Errorf("Env.State = %v, want LayerDisabled", r.Env.State)
	}
	if r.Env.Source != "env" {
		t.Errorf("Env.Source = %q, want \"env\" (constant per layer kind)", r.Env.Source)
	}
	if r.Env.EnvVar != "DO_NOT_TRACK" {
		t.Errorf("Env.EnvVar = %q, want \"DO_NOT_TRACK\"", r.Env.EnvVar)
	}
	if r.Env.Value != "1" {
		t.Errorf("Env.Value = %q, want \"1\"", r.Env.Value)
	}
	if !r.IsDisabled() {
		t.Error("IsDisabled = false, want true")
	}
}

func TestCheckOptOut_EnvUnsetHasStableSource(t *testing.T) {
	// Env layer's Source is the kind-constant ("env") regardless of
	// state. EnvVar is empty unless we triggered.
	store, _ := userconfig.Load(t.TempDir())
	r := checkOptOutWithEnv(store, t.TempDir(), os.TempDir(), emptyEnv)
	if r.Env.Source != "env" {
		t.Errorf("Env.Source = %q, want \"env\"", r.Env.Source)
	}
	if r.Env.EnvVar != "" {
		t.Errorf("Env.EnvVar = %q, want empty when Unset", r.Env.EnvVar)
	}
}

func TestCheckOptOut_EnvCanonicalOrder(t *testing.T) {
	// Plan pins canonical order: GAFFER_TELEMETRY_OPTOUT first, then
	// KURRENTDB_TELEMETRY_OPTOUT, then DO_NOT_TRACK. With all set,
	// the first found is reported.
	store, _ := userconfig.Load(t.TempDir())
	r := checkOptOutWithEnv(store, t.TempDir(), os.TempDir(), fixedEnv(map[string]string{
		"GAFFER_TELEMETRY_OPTOUT":    "true",
		"KURRENTDB_TELEMETRY_OPTOUT": "yes",
		"DO_NOT_TRACK":               "1",
	}))
	if r.Env.EnvVar != "GAFFER_TELEMETRY_OPTOUT" {
		t.Errorf("Env.EnvVar = %q, want GAFFER_TELEMETRY_OPTOUT (canonical first)", r.Env.EnvVar)
	}
}

func TestCheckOptOut_EnvFalsyDoesntOptOut(t *testing.T) {
	store, _ := userconfig.Load(t.TempDir())
	r := checkOptOutWithEnv(store, t.TempDir(), os.TempDir(), fixedEnv(map[string]string{
		"DO_NOT_TRACK":            "0",
		"GAFFER_TELEMETRY_OPTOUT": "false",
	}))
	if r.Env.State != LayerUnset {
		t.Errorf("Env.State = %v, want LayerUnset (falsy values don't disable)", r.Env.State)
	}
}

func TestCheckOptOut_WorkspaceDisabled(t *testing.T) {
	home := t.TempDir()
	proj := filepath.Join(home, "dev", "myproject")
	cwd := filepath.Join(proj, "src")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	gafferToml := filepath.Join(proj, "gaffer.toml")
	if err := os.WriteFile(gafferToml, []byte("telemetry = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, _ := userconfig.Load(t.TempDir())
	r := checkOptOutWithEnv(store, cwd, home, emptyEnv)
	if r.Workspace.State != LayerDisabled {
		t.Errorf("Workspace.State = %v, want LayerDisabled", r.Workspace.State)
	}
	if r.Workspace.Path != gafferToml {
		t.Errorf("Workspace.Path = %q, want %q", r.Workspace.Path, gafferToml)
	}
	if r.Workspace.Value != "false" {
		t.Errorf("Workspace.Value = %q, want \"false\"", r.Workspace.Value)
	}
}

func TestCheckOptOut_WorkspaceEnabledExplicit(t *testing.T) {
	home := t.TempDir()
	proj := filepath.Join(home, "dev", "myproject")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "gaffer.toml"), []byte("telemetry = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, _ := userconfig.Load(t.TempDir())
	r := checkOptOutWithEnv(store, proj, home, emptyEnv)
	if r.Workspace.State != LayerEnabled {
		t.Errorf("Workspace.State = %v, want LayerEnabled", r.Workspace.State)
	}
}

func TestCheckOptOut_StrayHomeGafferTomlIgnored(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "gaffer.toml"), []byte("telemetry = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(home, "dev")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	store, _ := userconfig.Load(t.TempDir())
	r := checkOptOutWithEnv(store, cwd, home, emptyEnv)
	if r.Workspace.State != LayerUnset {
		t.Errorf("Workspace.State = %v, want LayerUnset (stray ~/gaffer.toml must not apply)", r.Workspace.State)
	}
}

func TestCheckOptOut_TrailingSlashHomeStillBounds(t *testing.T) {
	// filepath.Clean strips the trailing slash so dir == stopAt holds
	// regardless of how the caller spelled the path.
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "gaffer.toml"), []byte("telemetry = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(home, "dev")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	store, _ := userconfig.Load(t.TempDir())
	r := checkOptOutWithEnv(store, cwd+"/", home+"/", emptyEnv)
	if r.Workspace.State != LayerUnset {
		t.Errorf("Workspace.State = %v, want LayerUnset (trailing slashes must not break the bound)", r.Workspace.State)
	}
}

func TestCheckOptOut_EmptyCWD(t *testing.T) {
	// cwd == "" short-circuits to LayerUnset before any walking.
	store, _ := userconfig.Load(t.TempDir())
	r := checkOptOutWithEnv(store, "", os.TempDir(), emptyEnv)
	if r.Workspace.State != LayerUnset {
		t.Errorf("Workspace.State = %v, want LayerUnset for empty cwd", r.Workspace.State)
	}
}

func TestCheckOptOut_AnySilences(t *testing.T) {
	dir := t.TempDir()
	store, _ := userconfig.Load(dir)
	on := true
	WriteTelemetry(store, TelemetrySection{Enabled: &on})

	r := checkOptOutWithEnv(store, t.TempDir(), os.TempDir(), fixedEnv(map[string]string{
		"DO_NOT_TRACK": "1",
	}))
	if r.User.State != LayerEnabled {
		t.Errorf("User.State = %v, want LayerEnabled", r.User.State)
	}
	if r.Env.State != LayerDisabled {
		t.Errorf("Env.State = %v, want LayerDisabled", r.Env.State)
	}
	if !r.IsDisabled() {
		t.Error("IsDisabled = false, want true (any-silences)")
	}
}

func TestCheckOptOut_MalformedProjectTomlSurfacesError(t *testing.T) {
	home := t.TempDir()
	proj := filepath.Join(home, "dev", "myproject")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "gaffer.toml"), []byte("not valid = toml = blah"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, _ := userconfig.Load(t.TempDir())
	r := checkOptOutWithEnv(store, proj, home, emptyEnv)
	if r.Workspace.State != LayerUnset {
		t.Errorf("Workspace.State = %v, want LayerUnset (fail-open on parse error)", r.Workspace.State)
	}
	if r.Workspace.Err == nil {
		t.Error("Workspace.Err = nil, want surfaced parse error")
	}
}

func TestCheckOptOut_NilStore(t *testing.T) {
	r := checkOptOutWithEnv(nil, t.TempDir(), os.TempDir(), fixedEnv(map[string]string{
		"DO_NOT_TRACK": "1",
	}))
	if r.User.State != LayerUnset {
		t.Errorf("User.State = %v, want LayerUnset for nil store", r.User.State)
	}
	if !r.IsDisabled() {
		t.Error("IsDisabled = false, want true (env disabled)")
	}
}

func TestResolved_EffectiveAnySilences(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    Resolved
		want LayerState
	}{
		{"all unset", Resolved{}, LayerUnset},
		{"only user enabled", Resolved{User: Layer{State: LayerEnabled}}, LayerEnabled},
		{"only env disabled", Resolved{Env: Layer{State: LayerDisabled}}, LayerDisabled},
		{"user enabled, env disabled", Resolved{User: Layer{State: LayerEnabled}, Env: Layer{State: LayerDisabled}}, LayerDisabled},
		{"workspace disabled overrides user enabled", Resolved{User: Layer{State: LayerEnabled}, Workspace: Layer{State: LayerDisabled}}, LayerDisabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.Effective(); got != tc.want {
				t.Errorf("Effective = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCheckOptOut_WorkspaceTomlWithoutTelemetryKeyHasNoPath(t *testing.T) {
	// gaffer.toml is present but doesn't declare a telemetry key:
	// "not set" outcome with empty Path (renderer shouldn't carry a
	// path that didn't carry a decision).
	home := t.TempDir()
	proj := filepath.Join(home, "dev", "myproject")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "gaffer.toml"), []byte("engine_version = 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, _ := userconfig.Load(t.TempDir())
	r := checkOptOutWithEnv(store, proj, home, emptyEnv)
	if r.Workspace.State != LayerUnset {
		t.Errorf("Workspace.State = %v, want LayerUnset", r.Workspace.State)
	}
	if r.Workspace.Path != "" {
		t.Errorf("Workspace.Path = %q, want empty for no-telemetry-key case", r.Workspace.Path)
	}
}

func TestCheckOptOut_WorkspaceUnreadableTomlSurfacesError(t *testing.T) {
	// Plant gaffer.toml with no read permission; resolveWorkspace
	// hits a non-IsNotExist error path. State=LayerUnset; Err set;
	// Path set so the renderer can name the file.
	if os.Geteuid() == 0 {
		t.Skip("root can read 0o000 files; skipping")
	}
	home := t.TempDir()
	proj := filepath.Join(home, "dev", "myproject")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	gafferToml := filepath.Join(proj, "gaffer.toml")
	if err := os.WriteFile(gafferToml, []byte("telemetry = false\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(gafferToml, 0o600) // let t.TempDir cleanup succeed
	store, _ := userconfig.Load(t.TempDir())
	r := checkOptOutWithEnv(store, proj, home, emptyEnv)
	if r.Workspace.State != LayerUnset {
		t.Errorf("Workspace.State = %v, want LayerUnset (fail-open on read error)", r.Workspace.State)
	}
	if r.Workspace.Err == nil {
		t.Error("Workspace.Err = nil, want surfaced read error")
	}
}

func TestCheckOptOut_UserNonTableSectionPropagatesErr(t *testing.T) {
	// Top-level `telemetry = "off"` is a structural error in the
	// user config. Layer.Err must surface for the status command.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("telemetry = \"off\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r := checkOptOutWithEnv(store, t.TempDir(), os.TempDir(), emptyEnv)
	if r.User.State != LayerUnset {
		t.Errorf("User.State = %v, want LayerUnset (fail-open)", r.User.State)
	}
	if r.User.Err == nil {
		t.Error("User.Err = nil, want surfaced structural error")
	}
}

func TestFindProjectRootBounded_EmptyStartReturnsEmpty(t *testing.T) {
	// Direct unit-test for the empty-start guard; the public path
	// (resolveWorkspaceLayer) also guards but this pins the helper's
	// contract independently.
	if got := findProjectRootBounded("", "/some/home"); got != "" {
		t.Errorf("findProjectRootBounded(\"\", _) = %q, want \"\"", got)
	}
}

func TestCheckOptOut_PublicEntryReadsProcessEnv(t *testing.T) {
	// Smoke test that the exported CheckOptOut wires os.LookupEnv.
	// Use t.Setenv since this single test is allowed to touch the
	// process env (test isolation maintained by t.Setenv's cleanup).
	t.Setenv("DO_NOT_TRACK", "1")
	store, _ := userconfig.Load(t.TempDir())
	r := CheckOptOut(store, t.TempDir(), os.TempDir())
	if !r.IsDisabled() {
		t.Error("CheckOptOut(public) did not see DO_NOT_TRACK=1 via os.LookupEnv")
	}
}
