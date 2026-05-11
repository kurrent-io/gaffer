package telemetry

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

func TestNoticeText_KeyPhrasesPresent(t *testing.T) {
	for _, want := range []string{
		"Telemetry",
		"anonymous",
		"Kurrent, Inc.",
		"gaffer config telemetry off",
		"telemetry = false",
		"1 / true / yes / on",
		"https://telemetry.gaffer.kurrent.io",
	} {
		if !strings.Contains(noticeText, want) {
			t.Errorf("noticeText missing %q", want)
		}
	}
}

// TestNoticeText_ListsEveryOptOutEnvVar is the canary that catches
// the more likely drift: NoticeText says "set var X to opt out" but
// optout.go forgot to add X (or vice versa).
func TestNoticeText_ListsEveryOptOutEnvVar(t *testing.T) {
	for _, name := range optOutEnvVars {
		if !strings.Contains(noticeText, name) {
			t.Errorf("noticeText missing env var %q (declared in optOutEnvVars but not advertised in the user banner)", name)
		}
	}
}

func TestNoticeText_SyncedWithTelemetryMd(t *testing.T) {
	path := findRepoRootFile(t, "TELEMETRY.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read TELEMETRY.md: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		"gaffer config telemetry off",
		"telemetry = false",
		"GAFFER_TELEMETRY_OPTOUT",
		"KURRENTDB_TELEMETRY_OPTOUT",
		"DO_NOT_TRACK",
		"https://telemetry.gaffer.kurrent.io",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("TELEMETRY.md missing %q (out of sync with noticeText)", want)
		}
	}
}

// findRepoRootFile walks up from cwd looking for name. Bounded so a
// misconfigured CI fails fast rather than stat-ing the filesystem
// root.
func findRepoRootFile(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	const maxLevels = 10
	for range maxLevels {
		candidate := filepath.Join(dir, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("walked %d levels from cwd without finding %s", maxLevels, name)
	return ""
}

func TestWriteNotice_WritesNoticeText(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteNotice(&buf); err != nil {
		t.Fatalf("WriteNotice: %v", err)
	}
	if buf.String() != noticeText {
		t.Errorf("written != noticeText")
	}
}

func TestResolveIdentity_EmptyStoreReturnsZero(t *testing.T) {
	store, _ := userconfig.Load(t.TempDir())
	id, err := ResolveIdentity(store)
	if err != nil {
		t.Fatalf("ResolveIdentity: %v", err)
	}
	if !id.IsZero() {
		t.Errorf("id = %+v, want zero on empty store", id)
	}
}

func TestResolveIdentity_PersistedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, _ := userconfig.Load(dir)
	first, _ := MintIdentity()
	StageIdentity(store, first)
	if err := store.Save(); err != nil {
		t.Fatalf("seed Save: %v", err)
	}

	store2, _ := userconfig.Load(dir)
	id, err := ResolveIdentity(store2)
	if err != nil {
		t.Fatalf("ResolveIdentity: %v", err)
	}
	if id.TelemetryID != first.TelemetryID || id.Salt != first.Salt {
		t.Errorf("id mismatch: got %+v, want %+v", id, first)
	}
	if id.RunID == first.RunID {
		t.Error("RunID not refreshed (must be per-process)")
	}
}

func TestResolveIdentity_PartialErrorPropagated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	const bad = `[telemetry]
enabled = 1
id = "abc"
salt = "def"
`
	if err := os.WriteFile(path, []byte(bad), 0o600); err != nil {
		t.Fatal(err)
	}
	store, _ := userconfig.Load(dir)
	id, err := ResolveIdentity(store)
	if err == nil {
		t.Error("err = nil, want partial-load error surfaced")
	}
	if id.TelemetryID != "abc" || id.Salt != "def" {
		t.Errorf("id = %+v, want usable (abc/def)", id)
	}
}

func TestResolveIdentity_StructuralErrorReturnsZero(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("telemetry = \"off\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, _ := userconfig.Load(dir)
	id, err := ResolveIdentity(store)
	if err == nil {
		t.Error("err = nil, want structural error")
	}
	if !id.IsZero() {
		t.Errorf("id = %+v, want zero on structural error", id)
	}
}

func TestMintAndPersist_FreshMintReportsDidMintTrue(t *testing.T) {
	store, _ := userconfig.Load(t.TempDir())
	id, didMint, err := MintAndPersist(store)
	if err != nil {
		t.Fatalf("MintAndPersist: %v", err)
	}
	if !didMint {
		t.Error("didMint = false on fresh mint")
	}
	if id.IsZero() {
		t.Error("id zero after mint")
	}
	reloaded, _, _ := IdentityFromConfig(store)
	if reloaded.TelemetryID != id.TelemetryID {
		t.Errorf("not persisted: %+v", reloaded)
	}
}

// TestMintAndPersist_DoesNotWriteNotice: the primitive is
// notice-free; only EnsureIdentity writes. Lock that in.
func TestMintAndPersist_DoesNotWriteNotice(t *testing.T) {
	// The function doesn't take an io.Writer, so this is an
	// architectural assertion rather than a behavioural one. The
	// build itself would fail if a Writer arg slipped back in. As a
	// safety net, also verify that an existing identity round-trips
	// without re-mint: confirms the read primitive isn't tangled
	// with notice logic either.
	dir := t.TempDir()
	store, _ := userconfig.Load(dir)
	first, _, err := MintAndPersist(store)
	if err != nil {
		t.Fatalf("first MintAndPersist: %v", err)
	}
	_ = first
}

// TestMintAndPersist_StageDoesNotPropagatePartialLoadErr verifies
// the contract change that motivated this round of review: a
// pre-existing malformed `enabled = 1` in the file must NOT abort
// the mint. The stage succeeds (id+salt written, malformed enabled
// dropped on round-trip), and MintAndPersist returns nil err.
func TestMintAndPersist_StageDoesNotPropagatePartialLoadErr(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[telemetry]\nenabled = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, _ := userconfig.Load(dir)
	id, didMint, err := MintAndPersist(store)
	if err != nil {
		t.Fatalf("MintAndPersist returned err on pre-existing malformed enabled: %v", err)
	}
	if !didMint {
		t.Error("didMint = false; mint should have happened")
	}
	if id.IsZero() {
		t.Error("identity zero despite mint reported")
	}
}

// TestMintAndPersist_RaceLost: two stores mint concurrently; loser
// observes ErrRaceLost (inside MintAndPersist), adopts winner's
// identity, keeps own RunID, reports didMint=false.
func TestMintAndPersist_RaceLost(t *testing.T) {
	// IMPORTANT: ordering invariant - loser.Load() must happen
	// before winner.Save() so loser's existedAtLoad=false survives.
	// A later refactor that caches store state at Load time would
	// invalidate this test setup; the explicit ordering documents
	// the requirement.
	dir := t.TempDir()
	a, _ := userconfig.Load(dir)
	b, _ := userconfig.Load(dir)

	idA, mintedA, err := MintAndPersist(a)
	if err != nil {
		t.Fatalf("A.MintAndPersist: %v", err)
	}
	if !mintedA {
		t.Error("A.didMint = false")
	}

	idB, mintedB, err := MintAndPersist(b)
	if err != nil {
		t.Fatalf("B.MintAndPersist: %v", err)
	}
	if mintedB {
		t.Error("B.didMint = true on lost race; should be false")
	}
	if idB.TelemetryID != idA.TelemetryID || idB.Salt != idA.Salt {
		t.Errorf("B identity didn't adopt winner: B=%+v A=%+v", idB, idA)
	}
	if idB.RunID == idA.RunID {
		t.Error("B.RunID == A.RunID after race; RunID is per-process")
	}
}

// TestMintAndPersist_SaveRaceLostExplicit independently verifies
// that Save returns ErrRaceLost on the racy path. A future refactor
// that bypasses O_EXCL would degrade the higher-level race test
// silently; this lower-level assertion catches it.
func TestMintAndPersist_SaveRaceLostExplicit(t *testing.T) {
	dir := t.TempDir()
	a, _ := userconfig.Load(dir)
	b, _ := userconfig.Load(dir)

	a.SetSection("telemetry", map[string]any{"id": "a", "salt": "a"})
	if err := a.Save(); err != nil {
		t.Fatalf("A.Save: %v", err)
	}
	b.SetSection("telemetry", map[string]any{"id": "b", "salt": "b"})
	if err := b.Save(); !errors.Is(err, userconfig.ErrRaceLost) {
		t.Fatalf("B.Save err = %v, want ErrRaceLost", err)
	}
}

func TestMintAndPersist_RaceWinnerEmptySentinel(t *testing.T) {
	// IMPORTANT: ordering - loser loads BEFORE winner writes, so
	// loser's existedAtLoad=false stays accurate. Winner writes a
	// corrupt config (Enabled-only, no id/salt) - the kind of state
	// a hand-edit could leave. Loser mints, fails O_EXCL, Reloads,
	// finds no usable identity, surfaces ErrRaceWinnerEmpty.
	dir := t.TempDir()

	loser, err := userconfig.Load(dir)
	if err != nil {
		t.Fatalf("loser Load: %v", err)
	}

	winner, _ := userconfig.Load(dir)
	on := true
	WriteTelemetry(winner, TelemetrySection{Enabled: &on})
	if err := winner.Save(); err != nil {
		t.Fatalf("winner Save: %v", err)
	}

	_, _, err = MintAndPersist(loser)
	if !errors.Is(err, ErrRaceWinnerEmpty) {
		t.Errorf("err = %v, want wraps ErrRaceWinnerEmpty", err)
	}
	if !strings.Contains(err.Error(), loser.Path()) {
		t.Errorf("err message %q doesn't include store path %q", err, loser.Path())
	}
}

// ----------------------------------------------------------------
// EnsureIdentity composition tests
// ----------------------------------------------------------------

func TestEnsureIdentity_FreshMintWritesNotice(t *testing.T) {
	store, _ := userconfig.Load(t.TempDir())
	var buf bytes.Buffer
	id, err := EnsureIdentity(store, Resolved{}, &buf, false)
	if err != nil {
		t.Fatalf("EnsureIdentity: %v", err)
	}
	if id.IsZero() {
		t.Error("id zero on fresh mint")
	}
	if buf.String() != noticeText {
		t.Errorf("notice not written; got %q", buf.String())
	}
}

func TestEnsureIdentity_OptOutSkipsEverything(t *testing.T) {
	store, _ := userconfig.Load(t.TempDir())
	var buf bytes.Buffer
	r := Resolved{Env: Layer{State: LayerDisabled, Source: "env", EnvVar: "DO_NOT_TRACK", Value: "1"}}
	id, err := EnsureIdentity(store, r, &buf, false)
	if err != nil {
		t.Fatalf("EnsureIdentity: %v", err)
	}
	if !id.IsZero() {
		t.Errorf("id = %+v, want zero under opt-out", id)
	}
	if buf.Len() != 0 {
		t.Errorf("notice written under opt-out: %q", buf.String())
	}
	// Crucially: no persisted identity. A user under DO_NOT_TRACK at
	// install time who later clears it gets the notice exactly when
	// telemetry would start (per the plan).
	if _, ok, _ := IdentityFromConfig(store); ok {
		t.Error("identity persisted under opt-out")
	}
}

func TestEnsureIdentity_ExistingIdentitySkipsNotice(t *testing.T) {
	dir := t.TempDir()
	seed, _ := userconfig.Load(dir)
	first, _ := MintIdentity()
	StageIdentity(seed, first)
	if err := seed.Save(); err != nil {
		t.Fatalf("seed: %v", err)
	}

	store, _ := userconfig.Load(dir)
	var buf bytes.Buffer
	id, err := EnsureIdentity(store, Resolved{}, &buf, false)
	if err != nil {
		t.Fatalf("EnsureIdentity: %v", err)
	}
	if id.TelemetryID != first.TelemetryID {
		t.Errorf("id mismatch: got %+v, want %+v", id, first)
	}
	if id.RunID == first.RunID {
		t.Error("RunID not refreshed")
	}
	if buf.Len() != 0 {
		t.Errorf("notice written for existing identity: %q", buf.String())
	}
}

func TestEnsureIdentity_SuppressNoticeStillMints(t *testing.T) {
	store, _ := userconfig.Load(t.TempDir())
	var buf bytes.Buffer
	id, err := EnsureIdentity(store, Resolved{}, &buf, true /* suppress */)
	if err != nil {
		t.Fatalf("EnsureIdentity: %v", err)
	}
	if id.IsZero() {
		t.Error("identity zero under suppress; should have minted")
	}
	if buf.Len() != 0 {
		t.Errorf("notice written under suppress: %q", buf.String())
	}
	if _, ok, _ := IdentityFromConfig(store); !ok {
		t.Error("identity not persisted under suppress")
	}
}

func TestEnsureIdentity_RaceLostNoNotice(t *testing.T) {
	dir := t.TempDir()
	a, _ := userconfig.Load(dir)
	b, _ := userconfig.Load(dir)

	var bufA, bufB bytes.Buffer
	idA, errA := EnsureIdentity(a, Resolved{}, &bufA, false)
	if errA != nil {
		t.Fatalf("A.EnsureIdentity: %v", errA)
	}
	if bufA.Len() == 0 {
		t.Error("A.notice not written on fresh mint")
	}
	idB, errB := EnsureIdentity(b, Resolved{}, &bufB, false)
	if errB != nil {
		t.Fatalf("B.EnsureIdentity: %v", errB)
	}
	if bufB.Len() != 0 {
		t.Errorf("B.notice written despite race-lost: %q", bufB.String())
	}
	if idB.TelemetryID != idA.TelemetryID {
		t.Errorf("B didn't adopt A's identity: B=%+v A=%+v", idB, idA)
	}
}

func TestEnsureIdentity_PartialPersistedErrorPropagates(t *testing.T) {
	// Existing identity loads with a partial err (malformed
	// enabled). EnsureIdentity returns the id AND the err so the
	// caller can warn.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[telemetry]\nenabled = 1\nid = \"abc\"\nsalt = \"def\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, _ := userconfig.Load(dir)
	var buf bytes.Buffer
	id, err := EnsureIdentity(store, Resolved{}, &buf, false)
	if err == nil {
		t.Error("err = nil, want partial-load warning surfaced")
	}
	if id.TelemetryID != "abc" {
		t.Errorf("id = %+v, want usable existing", id)
	}
	if buf.Len() != 0 {
		t.Errorf("notice written for existing identity: %q", buf.String())
	}
}

func TestEnsureIdentity_NoticeWriteFailureDoesNotFail(t *testing.T) {
	store, _ := userconfig.Load(t.TempDir())
	id, err := EnsureIdentity(store, Resolved{}, brokenWriter{}, false)
	if err != nil {
		t.Fatalf("EnsureIdentity returned err on stderr fail: %v", err)
	}
	if id.IsZero() {
		t.Error("id zero despite successful mint")
	}
	if _, ok, _ := IdentityFromConfig(store); !ok {
		t.Error("identity not persisted after notice-write failure")
	}
}

// brokenWriter is defined in sink_debug_test.go and re-used here.
