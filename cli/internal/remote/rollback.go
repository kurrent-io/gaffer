package remote

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
)

// VersionMatch is what a hash prefix resolved to in the projection's history:
// the definition to redeploy and its full content hash.
type VersionMatch struct {
	Def  *Definition
	Hash string
}

// RollbackRefusal refuses a target that differs from the deployed projection in
// a create-only dimension: rollback redeploys in place via Update, which carries
// only the query and emit, so an engine version or emitted-stream tracking
// change can't be applied. Nil when the target is applyable.
func RollbackRefusal(cmp deploy.Comparison, hash, name string) error {
	if !cmp.EngineVersionDiffers && !cmp.TrackEmittedStreamsDiffers {
		return nil
	}
	dim := "engine version"
	if !cmp.EngineVersionDiffers {
		dim = "emitted-stream tracking"
	}
	return fmt.Errorf("version %s differs from the deployed projection in %s, which rollback can't change in place; update local config and use `gaffer recreate %s`",
		shortHash(hash), dim, name)
}

// FindVersionByHash scans the projection's whole history for content whose hash
// matches the prefix. The same content at several versions is one match - the
// hash is the identity - so only a prefix straddling two different contents is
// ambiguous. Pages newest-first like the interactive timeline, each page bounded
// by the RPC timeout and the read capped by the history hard cap; the scan is
// bounded overall by the stream itself.
func (c *Client) FindVersionByHash(ctx context.Context, name, prefix string) (*VersionMatch, error) {
	matches := map[string]*Definition{}
	before := int64(-1)
	for {
		versions, err := c.readHistoryPage(ctx, name, before)
		if err != nil {
			return nil, err
		}
		if len(versions) == 0 {
			break
		}
		matchHashes(versions, prefix, matches)
		if scanSettled(prefix, matches) {
			break
		}
		oldest := versions[len(versions)-1].Number
		if oldest <= 0 {
			break
		}
		before = oldest
	}
	return resolveHashMatches(matches, prefix, name)
}

// readHistoryPage reads one hash-scan page under its own RPC deadline, so a
// stalled projections subsystem can't consume the whole scan's budget.
func (c *Client) readHistoryPage(ctx context.Context, name string, before int64) ([]Version, error) {
	pctx, cancel := context.WithTimeout(ctx, deploy.RPCTimeout)
	defer cancel()
	versions, _, err := c.ReadHistory(pctx, name, before, 0)
	return versions, err
}

// scanSettled reports whether older pages could still change the outcome: a
// full hash is exact, so one match settles it, and a second distinct content
// already proves the prefix ambiguous - either way the scan can stop early
// rather than paging a large history to its start.
func scanSettled(prefix string, matches map[string]*Definition) bool {
	return (len(prefix) == 64 && len(matches) > 0) || len(matches) > 1
}

// matchHashes collects the distinct contents in a page whose hash carries the
// prefix. Tombstones have no content of their own and are skipped.
func matchHashes(versions []Version, prefix string, matches map[string]*Definition) {
	for _, v := range versions {
		if v.Deleted || v.Definition == nil {
			continue
		}
		h := v.Definition.Descriptor().Hash()
		if strings.HasPrefix(h, prefix) {
			if _, ok := matches[h]; !ok {
				matches[h] = v.Definition
			}
		}
	}
}

// resolveHashMatches turns the collected matches into the single target, or the
// not-found / ambiguous-prefix error.
func resolveHashMatches(matches map[string]*Definition, prefix, name string) (*VersionMatch, error) {
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no version matching %q in the history of %s", prefix, name)
	case 1:
		for h, d := range matches {
			return &VersionMatch{Def: d, Hash: h}, nil
		}
	}
	hashes := make([]string, 0, len(matches))
	for h := range matches {
		hashes = append(hashes, shortHash(h))
	}
	slices.Sort(hashes)
	return nil, fmt.Errorf("%q matches %d different versions (%s); give more characters", prefix, len(matches), strings.Join(hashes, ", "))
}

// NormalizeHashPrefix validates a hash argument: lowercase hex, at least 4
// characters so a stray character can't match half the history, at most a full
// 64-character hash.
func NormalizeHashPrefix(s string) (string, error) {
	p := strings.ToLower(s)
	if len(p) < 4 {
		return "", fmt.Errorf("hash prefix %q is too short; give at least 4 characters", s)
	}
	if len(p) > 64 {
		return "", fmt.Errorf("hash prefix %q is longer than a full content hash", s)
	}
	for _, c := range p {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", fmt.Errorf("hash prefix %q is not hexadecimal", s)
		}
	}
	return p, nil
}

// shortHash abbreviates a full content hash to the leading 7 chars (git-style)
// for error messages; the full hash stays in machine output.
func shortHash(h string) string {
	if len(h) >= 7 {
		return h[:7]
	}
	return h
}
