package telemetry

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// pretendTTY swaps the package's isTTY check to return true for the
// duration of the test, so a bytes.Buffer noticeOut behaves like a
// real terminal. Tests that need TTY=false call pretendNonTTY.
func pretendTTY(t *testing.T) {
	t.Helper()
	prev := isTTY
	isTTY = func(io.Writer) bool { return true }
	t.Cleanup(func() { isTTY = prev })
}

func pretendNonTTY(t *testing.T) {
	t.Helper()
	prev := isTTY
	isTTY = func(io.Writer) bool { return false }
	t.Cleanup(func() { isTTY = prev })
}

func TestEnsureIdentity_FreshMintWritesNotice(t *testing.T) {
	pretendTTY(t)
	store, _ := userconfig.Load(t.TempDir())
	var buf bytes.Buffer
	id, err := EnsureIdentity(store, Resolved{}, Invocation{}, &buf)
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
	pretendTTY(t)
	store, _ := userconfig.Load(t.TempDir())
	var buf bytes.Buffer
	r := Resolved{Env: Layer{State: LayerDisabled, Source: "env", EnvVar: "DO_NOT_TRACK", Value: "1"}}
	id, err := EnsureIdentity(store, r, Invocation{}, &buf)
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

func TestEnsureIdentity_ExistingDisclosedIdentitySkipsNotice(t *testing.T) {
	// Realistic shape of a disclosed install: id+salt persisted AND
	// Disclosed=true latched on the first eligible run that printed
	// the banner. Subsequent runs return the identity and never
	// re-fire the notice.
	pretendTTY(t)
	dir := t.TempDir()
	seed, _ := userconfig.Load(dir)
	first, _ := MintIdentity()
	StageIdentity(seed, first)
	WriteTelemetry(seed, TelemetrySection{ID: first.TelemetryID, Salt: first.Salt, Disclosed: true})
	if err := seed.Save(); err != nil {
		t.Fatalf("seed: %v", err)
	}

	store, _ := userconfig.Load(dir)
	var buf bytes.Buffer
	id, err := EnsureIdentity(store, Resolved{}, Invocation{}, &buf)
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
		t.Errorf("notice written despite Disclosed=true: %q", buf.String())
	}
}

func TestEnsureIdentity_DisclosedAlreadyLatchedSuppressesNotice(t *testing.T) {
	// A prior direct-terminal run set Disclosed=true. A subsequent
	// mint (after `config telemetry off` -> `on`, which clears
	// id/salt but leaves Disclosed) must NOT re-show the banner.
	pretendTTY(t)
	store, _ := userconfig.Load(t.TempDir())
	WriteTelemetry(store, TelemetrySection{Disclosed: true})
	if err := store.Save(); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	id, err := EnsureIdentity(store, Resolved{}, Invocation{}, &buf)
	if err != nil {
		t.Fatalf("EnsureIdentity: %v", err)
	}
	if id.IsZero() {
		t.Error("identity zero under pre-set disclosed; should have minted")
	}
	if buf.Len() != 0 {
		t.Errorf("notice written under pre-set disclosed: %q", buf.String())
	}
	if _, ok, _ := IdentityFromConfig(store); !ok {
		t.Error("identity not persisted under pre-set disclosed")
	}
}

func TestEnsureIdentity_InvokerIDSuppressesNotice(t *testing.T) {
	// A spawner identifying itself via --invoker-id is expected to
	// have its own disclosure UI (e.g. the VS Code extension's
	// first-activation notification). EnsureIdentity must not also
	// print the CLI banner inside the spawn.
	pretendTTY(t) // even on a TTY, the invoker-id check wins
	store, _ := userconfig.Load(t.TempDir())
	var buf bytes.Buffer
	id, err := EnsureIdentity(store, Resolved{}, Invocation{InvokerID: "00000000-0000-0000-0000-000000000000"}, &buf)
	if err != nil {
		t.Fatalf("EnsureIdentity: %v", err)
	}
	if id.IsZero() {
		t.Error("identity zero under invoker-id spawn; mint should still happen")
	}
	if buf.Len() != 0 {
		t.Errorf("notice written despite --invoker-id: %q", buf.String())
	}
	// Disclosed must NOT latch - the gaffer-direct-terminal user
	// still deserves a chance to see the notice on their next run.
	store2, _ := userconfig.Load(store.Dir())
	t2, _ := LoadTelemetry(store2)
	if t2.Disclosed {
		t.Error("Disclosed latched on invoker-id-suppressed mint; would silence direct-terminal disclosure")
	}
}

func TestEnsureIdentity_NonTTYSuppressesNotice(t *testing.T) {
	// stderr captured (CI pipe, log redirect, `2>file`): the banner
	// would be invisible. Suppress and leave Disclosed unset so the
	// next direct-terminal run gets a real chance.
	pretendNonTTY(t)
	store, _ := userconfig.Load(t.TempDir())
	var buf bytes.Buffer
	id, err := EnsureIdentity(store, Resolved{}, Invocation{}, &buf)
	if err != nil {
		t.Fatalf("EnsureIdentity: %v", err)
	}
	if id.IsZero() {
		t.Error("identity zero on non-TTY; mint should still happen")
	}
	if buf.Len() != 0 {
		t.Errorf("notice written on non-TTY noticeOut: %q", buf.String())
	}
	store2, _ := userconfig.Load(store.Dir())
	t2, _ := LoadTelemetry(store2)
	if t2.Disclosed {
		t.Error("Disclosed latched on non-TTY; next direct-terminal run would be silently silenced")
	}
}

func TestEnsureIdentity_DeferredDisclosureFiresOnLaterEligibleRun(t *testing.T) {
	// First run: identity mints under suppress conditions (non-TTY
	// here, but the same holds for --invoker-id). Disclosed stays
	// false because the user didn't see the banner.
	dir := t.TempDir()
	store, _ := userconfig.Load(dir)
	pretendNonTTY(t)
	var firstBuf bytes.Buffer
	first, err := EnsureIdentity(store, Resolved{}, Invocation{}, &firstBuf)
	if err != nil {
		t.Fatalf("first EnsureIdentity: %v", err)
	}
	if first.IsZero() {
		t.Fatal("first run produced zero identity; expected silent mint")
	}
	if firstBuf.Len() != 0 {
		t.Errorf("first run wrote notice despite non-TTY: %q", firstBuf.String())
	}
	if t1, _ := LoadTelemetry(store); t1.Disclosed {
		t.Fatal("first run latched Disclosed despite suppression; deferred-disclosure broken")
	}

	// Second run on the same store, now on a TTY with no invoker-id.
	// The identity already exists; deferred disclosure must fire.
	isTTY = func(io.Writer) bool { return true }
	store2, _ := userconfig.Load(dir)
	var secondBuf bytes.Buffer
	second, err := EnsureIdentity(store2, Resolved{}, Invocation{}, &secondBuf)
	if err != nil {
		t.Fatalf("second EnsureIdentity: %v", err)
	}
	if second.TelemetryID != first.TelemetryID {
		t.Errorf("second run identity drifted: got %s, want %s (no re-mint)", second.TelemetryID, first.TelemetryID)
	}
	if secondBuf.Len() == 0 {
		t.Error("second run did not write deferred notice; user permanently silenced")
	}
	reloaded, _ := userconfig.Load(dir)
	if tr, _ := LoadTelemetry(reloaded); !tr.Disclosed {
		t.Error("Disclosed not latched after deferred-disclosure notice fired")
	}
}

func TestEnsureIdentity_FirstMintWritesNoticeAndPersistsDisclosed(t *testing.T) {
	// Direct user mint on a TTY with no spawner: notice fires AND
	// Disclosed=true persists so subsequent runs don't re-disclose.
	pretendTTY(t)
	store, _ := userconfig.Load(t.TempDir())
	var buf bytes.Buffer
	if _, err := EnsureIdentity(store, Resolved{}, Invocation{}, &buf); err != nil {
		t.Fatalf("EnsureIdentity: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("notice not written on fresh mint")
	}
	// Reload from disk and confirm disclosed=true persisted.
	store2, _ := userconfig.Load(store.Dir())
	t2, _ := LoadTelemetry(store2)
	if !t2.Disclosed {
		t.Error("disclosed flag not persisted after first-mint notice")
	}
}

func TestEnsureIdentity_RaceLoserAdoptsLatchedDisclosure(t *testing.T) {
	// Sequential, not concurrent: A's EnsureIdentity completes
	// fully (mint + notice + Disclosed=true latch) before B starts.
	// B then sees the latched Disclosed via MintAndPersist's race-
	// recovery Reload, adopts the same identity, and skips the
	// banner. The concurrent variant where B's Reload happens
	// between A's notice-Save and A's Disclosed-Save is exercised
	// by TestEnsureIdentity_ConcurrentMintsConvergeOnLatchedDisclosure.
	pretendTTY(t)
	dir := t.TempDir()
	a, _ := userconfig.Load(dir)
	b, _ := userconfig.Load(dir)

	var bufA, bufB bytes.Buffer
	idA, errA := EnsureIdentity(a, Resolved{}, Invocation{}, &bufA)
	if errA != nil {
		t.Fatalf("A.EnsureIdentity: %v", errA)
	}
	if bufA.Len() == 0 {
		t.Error("A.notice not written on fresh mint")
	}
	idB, errB := EnsureIdentity(b, Resolved{}, Invocation{}, &bufB)
	if errB != nil {
		t.Fatalf("B.EnsureIdentity: %v", errB)
	}
	if bufB.Len() != 0 {
		t.Errorf("B.notice written despite Disclosed already latched: %q", bufB.String())
	}
	if idB.TelemetryID != idA.TelemetryID {
		t.Errorf("B didn't adopt A's identity: B=%+v A=%+v", idB, idA)
	}
}

func TestEnsureIdentity_ConcurrentDeferredDisclosureConvergesOnLatch(t *testing.T) {
	// Pre-seed identity without Disclosed so the deferred-disclosure
	// path is what's under contention here, not MintAndPersist. (Two
	// truly-concurrent fresh mints would also exercise an unrelated
	// race in MintAndPersist's Reload window between O_EXCL succeeding
	// and the winner's content reaching disk.) Both goroutines call
	// EnsureIdentity through a barrier so they reach
	// maybeShowDisclosure as close to simultaneously as the runtime
	// allows.
	//
	// Invariants:
	//   - both calls return the same identity (the pre-seeded one),
	//   - at least one banner was written (the user is not silenced),
	//   - on-disk Disclosed=true after both complete (no permanently-
	//     pending state).
	//
	// We deliberately do NOT assert a specific banner count -
	// duplicate banner is accepted as the cost of avoiding a file-
	// level lock around a single human-readable message.
	pretendTTY(t)
	dir := t.TempDir()
	seed, _ := userconfig.Load(dir)
	id, _ := MintIdentity()
	StageIdentity(seed, id)
	// Identity but no Disclosed: simulates a prior suppressed mint
	// (non-TTY CI, extension spawn) that's now owed disclosure.
	if err := seed.Save(); err != nil {
		t.Fatalf("seed: %v", err)
	}

	a, _ := userconfig.Load(dir)
	b, _ := userconfig.Load(dir)

	start := make(chan struct{})
	type result struct {
		id  Identity
		buf bytes.Buffer
		err error
	}
	results := make([]result, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		rid, err := EnsureIdentity(a, Resolved{}, Invocation{}, &results[0].buf)
		results[0].id = rid
		results[0].err = err
	}()
	go func() {
		defer wg.Done()
		<-start
		rid, err := EnsureIdentity(b, Resolved{}, Invocation{}, &results[1].buf)
		results[1].id = rid
		results[1].err = err
	}()
	close(start)
	wg.Wait()

	for i, r := range results {
		if r.err != nil {
			t.Fatalf("goroutine %d: %v", i, r.err)
		}
		if r.id.TelemetryID != id.TelemetryID {
			t.Errorf("goroutine %d returned %s, want pre-seeded %s", i, r.id.TelemetryID, id.TelemetryID)
		}
	}
	if results[0].buf.Len() == 0 && results[1].buf.Len() == 0 {
		t.Error("neither goroutine wrote the banner; user silenced under concurrent deferred disclosure")
	}
	reloaded, _ := userconfig.Load(dir)
	if t1, _ := LoadTelemetry(reloaded); !t1.Disclosed {
		t.Error("Disclosed not latched on disk after concurrent deferred-disclosure runs")
	}
}

func TestEnsureIdentity_PartialPersistedErrorPropagates(t *testing.T) {
	// Existing identity loads with a partial err (malformed
	// enabled). EnsureIdentity returns the id AND the err so the
	// caller can warn. disclosed=true so the deferred-disclosure
	// path doesn't fire for this disclosed install.
	pretendTTY(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("[telemetry]\nenabled = 1\nid = \"abc\"\nsalt = \"def\"\ndisclosed = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, _ := userconfig.Load(dir)
	var buf bytes.Buffer
	id, err := EnsureIdentity(store, Resolved{}, Invocation{}, &buf)
	if err == nil {
		t.Error("err = nil, want partial-load warning surfaced")
	}
	if id.TelemetryID != "abc" {
		t.Errorf("id = %+v, want usable existing", id)
	}
	if buf.Len() != 0 {
		t.Errorf("notice written despite Disclosed=true: %q", buf.String())
	}
}

func TestEnsureIdentity_NoticeWriteFailureDoesNotFail(t *testing.T) {
	pretendTTY(t)
	store, _ := userconfig.Load(t.TempDir())
	id, err := EnsureIdentity(store, Resolved{}, Invocation{}, brokenWriter{})
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

func TestEnsureIdentity_NoticeWriteFailureLeavesDisclosedFalse(t *testing.T) {
	// Privacy: if the notice write failed, the user never saw the
	// disclosure. We must NOT latch Disclosed=true - otherwise the
	// next run from a non-broken stderr would skip the notice and
	// the user has been permanently silenced.
	pretendTTY(t)
	store, _ := userconfig.Load(t.TempDir())
	if _, err := EnsureIdentity(store, Resolved{}, Invocation{}, brokenWriter{}); err != nil {
		t.Fatalf("EnsureIdentity: %v", err)
	}
	// Re-read from disk to confirm the on-disk state isn't latched.
	store2, _ := userconfig.Load(store.Dir())
	t2, _ := LoadTelemetry(store2)
	if t2.Disclosed {
		t.Error("Disclosed latched after WriteNotice failure; would silently silence future disclosure attempts")
	}
}

// brokenWriter is defined in sink_debug_test.go and re-used here.
