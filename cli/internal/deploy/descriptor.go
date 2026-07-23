// Package deploy holds the comparison core shared by gaffer diff, status, and
// deploy: a canonical descriptor of a projection's deployable definition, its
// content hash, and the in-sync/drifted verdict.
//
// It is a pure leaf - no engine or remote dependencies, so importing it never
// pulls in the cgo runtime. Each consumer assembles a Descriptor from its own
// side: the deployed side from a read-back remote.Definition (a field copy), the
// local side via engine (which compiles to derive emit and so owns that builder).
//
// The server stores the query verbatim, so the canonicalisation here is local-
// build determinism - it decides that certain editor-introduced byte deltas
// (BOM, CRLF vs LF, trailing newlines) are behaviourally inert and not drift -
// rather than compensation for any server-side transformation of the query.
package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Descriptor reduces a projection's deployable definition to the fields that
// decide whether local and deployed agree. Query, EngineVersion and Emit are the
// obvious ones; TrackEmittedStreams is included because it's a create-time option
// with no update path (V1 only), so a change to it forces delete-and-recreate and
// deploy must detect it. Lifecycle state (enabled/paused, run position) is
// deliberately excluded - that's status, not definition drift.
type Descriptor struct {
	Query               string
	EngineVersion       int
	Emit                bool
	TrackEmittedStreams bool
}

// Hash is a stable content hash over the descriptor, for a cheap equality check
// (status's drift column, deploy's idempotency skip). The query is canonicalised
// first, so a Hash match means Compare reports in-sync and vice versa.
func (d Descriptor) Hash() string {
	// Labelled fields first, the canonical query last, so a query that happens to
	// look like a field line can't collide with the field encoding.
	payload := fmt.Sprintf("engineVersion=%d\nemit=%t\ntrackEmittedStreams=%t\n%s",
		d.EngineVersion, d.Emit, d.TrackEmittedStreams, canonicalQuery(d.Query))
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// CanonicalQuery returns the query in the form Hash and Compare judge it by, so a
// diff viewer renders exactly what the verdict is based on (otherwise a CRLF-only
// delta reads as in-sync yet the viewer shows the raw bytes differing).
func (d Descriptor) CanonicalQuery() string {
	return canonicalQuery(d.Query)
}

// canonicalQuery strips a leading UTF-8 BOM, normalises line endings to LF, and
// trims to exactly one trailing newline - the byte differences an editor adds
// without changing the JS. Nothing else is normalised; over-normalising would
// hide real changes. A lone CR (not part of CRLF) and trailing spaces are left
// intact, so they count as drift.
func canonicalQuery(q string) string {
	q = strings.TrimPrefix(q, "\uFEFF")
	q = strings.ReplaceAll(q, "\r\n", "\n")
	return strings.TrimRight(q, "\n") + "\n"
}

// Comparison reports which dimensions of two descriptors differ, so a caller can
// message precisely and only open a source diff when the query itself differs. It
// reports which dimensions changed, not the values - the caller holds both
// Descriptors and reads the values (e.g. "engine version 1 -> 2") from those.
type Comparison struct {
	QueryDiffers               bool
	EngineVersionDiffers       bool
	EmitDiffers                bool
	TrackEmittedStreamsDiffers bool
}

// InSync reports whether the descriptors agree on every dimension.
func (c Comparison) InSync() bool {
	return !c.QueryDiffers && !c.EngineVersionDiffers && !c.EmitDiffers && !c.TrackEmittedStreamsDiffers
}

// ChangeSummary names the dimensions that differ, e.g. "query changed" or
// "query and emit changed" - what a content change reads as. Falls back to
// "definition changed" when no dimension is named (a defensive default).
func (c Comparison) ChangeSummary() string {
	var dims []string
	if c.QueryDiffers {
		dims = append(dims, "query")
	}
	if c.EngineVersionDiffers {
		dims = append(dims, "engine version")
	}
	if c.EmitDiffers {
		dims = append(dims, "emit")
	}
	if c.TrackEmittedStreamsDiffers {
		dims = append(dims, "tracking")
	}
	if len(dims) == 0 {
		return "definition changed"
	}
	return joinAnd(dims) + " changed"
}

// joinAnd joins items as "a", "a and b", or "a, b and c".
func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
	}
}

// Compare reports how a local descriptor differs from the deployed one. The query
// comparison uses the canonical form, so it agrees with Hash.
func Compare(local, deployed Descriptor) Comparison {
	return Comparison{
		QueryDiffers:               canonicalQuery(local.Query) != canonicalQuery(deployed.Query),
		EngineVersionDiffers:       local.EngineVersion != deployed.EngineVersion,
		EmitDiffers:                local.Emit != deployed.Emit,
		TrackEmittedStreamsDiffers: local.TrackEmittedStreams != deployed.TrackEmittedStreams,
	}
}
