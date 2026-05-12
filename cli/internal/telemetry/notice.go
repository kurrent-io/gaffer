package telemetry

import (
	"errors"
	"fmt"
	"io"

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

// EnsureIdentity is the composition entry point: opt-out check,
// existing-identity load, mint if needed, notice on fresh mint.
//
// Behaviour:
//   - opt-out active: returns zero Identity, no mint, no notice.
//   - existing usable identity: returns it (paired with fresh RunID),
//     no mint, no notice. Any partial-load error from
//     ResolveIdentity propagates to the caller.
//   - no existing identity + not opted out: mints, persists, and
//     (unless suppressNotice is true) writes noticeText to
//     noticeOut.
//   - race-lost during mint: adopts winner's id, does NOT write the
//     notice (winner already did in their process).
//
// suppressNotice is set by the caller when --invoker-id was passed.
// The spawning surface (typically the VS Code extension's
// first-activation notification) has already disclosed; printing to
// a non-TTY stderr inside an extension-spawned CLI would be
// invisible anyway. See UI-1561 for the extension-side disclosure
// gate.
//
// A WriteNotice failure (stderr broken) is silently dropped: the
// mint succeeded and failing the whole run would be worse UX than a
// missed disclosure.
//
// Returns (id, err). Use id.IsZero() to gate emit; err may be
// non-nil with a usable id (partial parse warning) - caller chooses
// to surface or ignore.
func EnsureIdentity(
	store *userconfig.Store,
	optOut Resolved,
	noticeOut io.Writer,
	suppressNotice bool,
) (Identity, error) {
	if optOut.IsDisabled() {
		return Identity{}, nil
	}
	if existing, err := ResolveIdentity(store); !existing.IsZero() {
		return existing, err
	}
	id, didMint, err := MintAndPersist(store)
	if err != nil {
		return Identity{}, err
	}
	if didMint && !suppressNotice {
		// Best-effort. See WriteNotice / MintAndPersist docs for the
		// failure-policy rationale.
		_ = WriteNotice(noticeOut)
	}
	return id, nil
}
