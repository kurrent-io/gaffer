package telemetry

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

// noticeText is the canonical first-mint disclosure printed to stderr
// when telemetry starts emitting for the first time. The same wording
// appears in TELEMETRY.md's Disclosure section; a sync canary in the
// test suite asserts the key phrases appear in both, so this constant
// and the doc stay in lockstep.
//
// Terse on purpose: one banner the user can scan at a glance, listing
// the three opt-out paths the CLI supports. (VS Code's
// `telemetry.telemetryLevel` is honoured by the extension too but is
// the extension's UI concern; documenting it here would only confuse
// a terminal-only user.)
//
// Unexported: callers go through WriteNotice or EnsureIdentity - the
// raw string isn't part of the package's API surface.
const noticeText = `Telemetry
---------
Gaffer collects usage data in order to improve your experience. The data is anonymous and collected by Kurrent, Inc.

You can opt out by any of:
  - Running ` + "`gaffer config telemetry off`" + ` (this machine)
  - Adding ` + "`telemetry = false`" + ` to your project's gaffer.toml
  - Setting GAFFER_TELEMETRY_OPTOUT, KURRENTDB_TELEMETRY_OPTOUT, or DO_NOT_TRACK to 1 / true / yes / on

For more information visit https://telemetry.gaffer.kurrent.io.
`

// ErrRaceWinnerEmpty is returned by MintAndPersist when a concurrent
// process wrote a [telemetry] section without populating an id/salt
// pair (e.g. a fat-fingered manual edit). Should not happen for a
// gaffer-written config; the sentinel lets callers detect this
// specific corruption rather than treat it as a generic mint
// failure.
var ErrRaceWinnerEmpty = errors.New("telemetry: race recovery: winner persisted no identity")

// WriteNotice writes the canonical first-mint notice to w. Returns
// any write error verbatim; the caller decides whether to surface it
// (typical CLI use ignores it: a stderr that's broken has bigger
// problems than a missed telemetry notice).
func WriteNotice(w io.Writer) error {
	_, err := io.WriteString(w, noticeText)
	return err
}

// ResolveIdentity loads the persisted identity from store and pairs
// it with a fresh RunID.
//
// CALLERS MUST NOT TREAT `err == nil` AS "identity is good". The four
// return shapes are:
//
//   - (Identity{}, nil) - no [telemetry] id/salt persisted. Caller
//     decides whether to mint (typically only if opt-out is not
//     active).
//   - (id, nil) - usable identity loaded cleanly.
//   - (id, err) - usable identity loaded but the [telemetry] section
//     had a partial parse error (e.g. malformed enabled key).
//     Callers that just want to emit can ignore err; the
//     `gaffer config telemetry status` path surfaces it so the user
//     can fix the file.
//   - (Identity{}, err) - load failed structurally. No usable
//     identity; caller decides what to do.
//
// Use IsZero on the returned Identity to distinguish "no identity"
// from "identity loaded".
//
// ResolveIdentity does NOT check opt-out - that's the caller's
// (typically EnsureIdentity's) job. Returning the persisted identity
// when opt-out is active would tempt callers to emit despite the
// user's preference.
func ResolveIdentity(store *userconfig.Store) (Identity, error) {
	id, ok, err := IdentityFromConfig(store)
	if !ok {
		return Identity{}, err
	}
	return id, err
}

// MintAndPersist mints fresh credentials, persists them to store,
// and recovers from the first-write race. It does NOT write the
// notice or check opt-out - those are the caller's concerns. Use
// EnsureIdentity for the dominant flow; this primitive is for tests
// and for the rare "force a mint" path.
//
// Returns:
//   - (id, true, nil) - this call did the mint. Caller is the one
//     that "owns" the fresh identity for notice purposes.
//   - (id, false, nil) - lost the first-write race; adopted the
//     winner's persisted id+salt. Kept the freshly minted RunID -
//     RunIDs are per-process, not per-install. Caller should NOT
//     write the notice (the winner already did).
//   - (Identity{}, false, err) - the mint or persistence failed.
//
// Race recovery: if Save returns ErrRaceLost, Reload, adopt the
// winner's id+salt from disk, and return didMint=false.
func MintAndPersist(store *userconfig.Store) (Identity, bool, error) {
	fresh, err := MintIdentity()
	if err != nil {
		return Identity{}, false, fmt.Errorf("telemetry: mint: %w", err)
	}
	StageIdentity(store, fresh)
	if err := store.Save(); err != nil {
		if !errors.Is(err, userconfig.ErrRaceLost) {
			return Identity{}, false, fmt.Errorf("telemetry: save: %w", err)
		}
		// Lost the first-mint race. Reload + adopt.
		if reloadErr := store.Reload(); reloadErr != nil {
			return Identity{}, false, fmt.Errorf("telemetry: race recovery reload: %w (path=%s)", reloadErr, store.Path())
		}
		// IdentityFromConfig may also return a partial err (winner's
		// section had its own minor issues). Drop it deliberately:
		// the loser isn't the right surface for those warnings, and
		// the user will see them on the next run via ResolveIdentity.
		winner, ok, _ := IdentityFromConfig(store)
		if !ok {
			return Identity{}, false, fmt.Errorf("%w (path=%s)", ErrRaceWinnerEmpty, store.Path())
		}
		return Identity{
			TelemetryID: winner.TelemetryID,
			Salt:        winner.Salt,
			RunID:       fresh.RunID,
		}, false, nil
	}
	return fresh, true, nil
}

// shouldShowDisclosureNotice decides whether to print the disclosure
// banner to noticeOut for the in-flight run. The check runs on every
// invocation that reaches identity-resolution (whether or not a mint
// happened this run), so a first mint that wasn't an eligible
// disclosure surface (non-TTY CI, extension spawn) gets caught up
// by the next direct-terminal run while Disclosed is still false.
//
// Three suppress signals; any one trips the no-print branch:
//
//   - Disclosed already latched: a prior run on this machine showed
//     the notice and recorded ack; don't repeat (the Disclosed flag
//     survives `config telemetry off` -> `on` round-trips, by design).
//   - inv.InvokerID set: a parent process is identifying itself as
//     the spawner. The spawner is expected to surface its own
//     telemetry disclosure (the VS Code extension does this on first
//     activation). Re-printing on stderr inside the spawn would be
//     invisible or duplicative anyway.
//   - noticeOut isn't a TTY: a captured / redirected / piped stderr
//     would swallow the banner. Re-checking on the next direct-
//     terminal run gives the user a real chance to see it.
//
// The decision is intentionally stateless beyond reading the
// persisted Disclosed flag: no caller-set "quiet" flag can suppress
// disclosure, no caller-set state can latch Disclosed without
// actually showing the notice.
func shouldShowDisclosureNotice(store *userconfig.Store, inv Invocation, noticeOut io.Writer) bool {
	if t, _ := LoadTelemetry(store); t.Disclosed {
		return false
	}
	if inv.InvokerID != "" {
		return false
	}
	return isTTY(noticeOut)
}

// IsTTYCheckForTesting overrides the TTY check used by
// EnsureIdentity / shouldShowDisclosureNotice for the duration of
// the returned restore func. Test-only export: cmd-package tests
// run with stderr captured into a bytes.Buffer, which trips the
// non-TTY suppress branch and hides the notice; tests that exercise
// the notice path replace the check. Do NOT call from production.
func IsTTYCheckForTesting(fn func(io.Writer) bool) (restore func()) {
	prev := isTTY
	isTTY = fn
	return func() { isTTY = prev }
}

// isTTY reports whether w is backed by a terminal. Returns false for
// any non-*os.File (bytes.Buffer in tests, io.MultiWriter, etc.) and
// for *os.File pointing at a pipe, regular file, or /dev/null. Uses
// the char-device bit on the file's Mode - matches `git`,
// `kubectl`, and most CLIs' "am I piped?" check; portable across
// Linux / macOS / Windows console handles without a TTY-detection
// dependency.
//
// Package-level var so tests can override (test stderr is a
// bytes.Buffer that'd never trip the char-device check otherwise).
var isTTY = func(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	stat, err := f.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice != 0
}

// EnsureIdentity is the composition entry point: opt-out check,
// existing-identity load, mint if needed, deferred disclosure on
// any eligible run.
//
// Behaviour:
//   - opt-out active: returns zero Identity, no mint, no notice.
//   - existing usable identity: returns it (paired with fresh RunID).
//     Notice fires here if Disclosed is still false and conditions
//     allow - catches up after a first mint that was suppressed
//     (non-TTY CI, extension spawn). Partial-load errors from
//     ResolveIdentity propagate to the caller.
//   - no existing identity + not opted out: mints, persists, then
//     attempts the notice on the same conditions as above.
//
// The notice attempt is the same code path whether the identity is
// fresh or pre-existing - "first mint" is a misleading name for the
// suppress signals, because the user's first eligible disclosure
// surface may not be the same process that minted.
//
// inv carries the spawn-linkage parsed from --invoker-id /
// --invoked-by / --invoked-via. Only InvokerID is consulted here
// (presence => suppress); the other two affect emit-side stamping.
//
// A WriteNotice or Save failure is silently dropped: the mint
// succeeded and failing the whole run would be worse UX than a
// missed disclosure.
//
// Returns (id, err). Use id.IsZero() to gate emit; err may be
// non-nil with a usable id (partial parse warning) - caller chooses
// to surface or ignore.
func EnsureIdentity(
	store *userconfig.Store,
	optOut Resolved,
	inv Invocation,
	noticeOut io.Writer,
) (Identity, error) {
	if optOut.IsDisabled() {
		return Identity{}, nil
	}
	id, loadErr := ResolveIdentity(store)
	if id.IsZero() {
		minted, _, mintErr := MintAndPersist(store)
		if mintErr != nil {
			return Identity{}, mintErr
		}
		id = minted
	}
	maybeShowDisclosure(store, inv, noticeOut)
	return id, loadErr
}

// maybeShowDisclosure prints the notice and latches Disclosed=true
// when shouldShowDisclosureNotice allows. Only latches on a
// SUCCESSFUL write: if stderr is broken (redirected /dev/null mid-
// stream, captured-and-dropped wrapper) the user never saw the
// banner, so a later eligible run still gets a chance. Without this
// gating a single bad stderr would permanently silence future
// disclosure attempts.
//
// Read-modify-write of [telemetry] is in-memory (LoadTelemetry
// reparses the store's already-loaded section, not disk). Relies on
// the invariant that only EnsureIdentity / MintAndPersist mutate
// [telemetry] within a process - no other field is read here, no
// other code path writes Disclosed.
//
// Concurrent first runs (two CLI processes on a fresh install in
// parallel) may both print the banner: the loser's race-recovery
// Reload inside MintAndPersist captures the disk state before the
// winner has latched Disclosed, so both call WriteNotice. The
// O_EXCL + temp+rename Save makes the on-disk Disclosed=true write
// last-writer-wins safe; duplicate banner is accepted as the cost
// of avoiding a file-level lock around a single human-readable
// message. See TestEnsureIdentity_ConcurrentMintsConvergeOnLatchedDisclosure.
func maybeShowDisclosure(store *userconfig.Store, inv Invocation, noticeOut io.Writer) {
	if !shouldShowDisclosureNotice(store, inv, noticeOut) {
		return
	}
	if err := WriteNotice(noticeOut); err != nil {
		return
	}
	t, _ := LoadTelemetry(store)
	t.Disclosed = true
	WriteTelemetry(store, t)
	_ = store.Save()
}
