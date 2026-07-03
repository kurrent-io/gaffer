package drift

import (
	"context"
	"errors"
	"slices"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// StatusEntry is one projection's runtime state plus how it compares to local.
// Runtime is nil when the projection isn't deployed.
type StatusEntry struct {
	Comparison
	Runtime *remote.Status
}

// StatusOne resolves a single projection's drift and (if deployed) runtime state.
func StatusOne(ctx context.Context, r *remote.Client, cfg *config.Config, root, name string) (StatusEntry, error) {
	cmp, err := Compare(ctx, r, cfg, root, name)
	if err != nil {
		return StatusEntry{}, err
	}
	e := StatusEntry{Comparison: cmp}
	// Fetch runtime state only when the projection exists on the server. Gating on
	// Deployed (not the drift state) covers Invalid: a projection that won't
	// compile and isn't deployed has no runtime to ask for.
	if cmp.Deployed != nil {
		st, err := r.Status(ctx, name)
		if err != nil && !errors.Is(err, remote.ErrNotFound) {
			return StatusEntry{}, err
		}
		e.Runtime = st
	}
	return e, nil
}

// StatusAll reconciles every local and deployed projection into status
// entries: tracked (runtime + drift), not-deployed (local only), and untracked
// (deployed, not in config).
func StatusAll(ctx context.Context, r *remote.Client, cfg *config.Config, root string) ([]StatusEntry, error) {
	deployed, err := r.List(ctx)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]remote.Status, len(deployed))
	for i := range deployed {
		byName[deployed[i].Name] = deployed[i]
	}

	local := make(map[string]bool, len(cfg.Projection))
	var entries []StatusEntry
	for i := range cfg.Projection {
		name := cfg.Projection[i].Name
		local[name] = true
		cmp, err := Compare(ctx, r, cfg, root, name)
		if err != nil {
			return nil, err
		}
		e := StatusEntry{Comparison: cmp}
		if rt, ok := byName[name]; ok {
			e.Runtime = &rt
		}
		entries = append(entries, e)
	}

	var untracked []string
	for i := range deployed {
		if !local[deployed[i].Name] {
			untracked = append(untracked, deployed[i].Name)
		}
	}
	slices.Sort(untracked)
	for _, n := range untracked {
		// Route through Compare (its not-in-config branch) so an untracked
		// projection's ledger read and ownership classification follow the same path
		// as the tracked ones, rather than being re-derived here.
		cmp, err := Compare(ctx, r, cfg, root, n)
		if errors.Is(err, remote.ErrNotFound) {
			// Deleted between the List above and this read - a benign race, so skip
			// it rather than failing the whole overview.
			continue
		}
		if err != nil {
			return nil, err
		}
		rt := byName[n]
		entries = append(entries, StatusEntry{Comparison: cmp, Runtime: &rt})
	}
	return entries, nil
}
