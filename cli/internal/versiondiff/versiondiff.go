// Package versiondiff resolves and diffs any two versions of a projection - a
// content hash, the deployed version, or the local definition - into the
// structured cliout.DiffJSON. Shared by `gaffer diff --left/--right` and the
// language server's gaffer/diffVersions request so both resolve refs and build
// the diff identically.
package versiondiff

import (
	"context"
	"errors"
	"fmt"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// RefKind is which version a --left/--right value names.
type RefKind int

const (
	RefDeployed RefKind = iota
	RefLocal
	RefHash
)

// Ref is a parsed --left/--right value. Hash is the normalised prefix, set only
// for RefHash.
type Ref struct {
	Kind RefKind
	Hash string
}

// ParseRef parses a ref string: "deployed", "local", or a content-hash prefix.
func ParseRef(s string) (Ref, error) {
	switch s {
	case "deployed":
		return Ref{Kind: RefDeployed}, nil
	case "local":
		return Ref{Kind: RefLocal}, nil
	default:
		p, err := remote.NormalizeHashPrefix(s)
		if err != nil {
			return Ref{}, fmt.Errorf("invalid ref %q: use 'local', 'deployed', or a content-hash prefix (%w)", s, err)
		}
		return Ref{Kind: RefHash, Hash: p}, nil
	}
}

// ResolvedSide is one operand resolved to its JSON shape plus a short label for
// text output and external-diff filenames. Uncompiled carries the compile error
// when a local side's source doesn't compile - the source still diffs, but the
// caller surfaces the failure rather than swallow it.
type ResolvedSide struct {
	JSON       cliout.DiffSideJSON
	Label      string
	Uncompiled error
}

// ResolveSide reads one side of a version diff. deployed and a hash come from the
// server; local is built from gaffer.toml. A local definition that doesn't
// compile still yields its source (from the partial descriptor) but no hash -
// emit is unknown, so the hash would be misleading.
func ResolveSide(ctx context.Context, r *remote.Client, cfg *config.Config, root, name string, ref Ref) (ResolvedSide, error) {
	switch ref.Kind {
	case RefDeployed:
		def, err := r.Read(ctx, name)
		if errors.Is(err, remote.ErrNotFound) {
			return ResolvedSide{}, fmt.Errorf("%q is not deployed", name)
		}
		if err != nil {
			return ResolvedSide{}, err
		}
		d := def.Descriptor()
		return ResolvedSide{JSON: cliout.DiffSideJSON{Ref: "deployed", Hash: d.Hash(), Source: d.CanonicalQuery()}, Label: "deployed"}, nil
	case RefHash:
		m, err := r.FindVersionByHash(ctx, name, ref.Hash)
		if err != nil {
			return ResolvedSide{}, err
		}
		d := m.Def.Descriptor()
		return ResolvedSide{JSON: cliout.DiffSideJSON{Ref: "version", Hash: m.Hash, Source: d.CanonicalQuery()}, Label: ShortRef(m.Hash)}, nil
	default: // RefLocal
		d, compileErr, err := localDescriptor(cfg, root, name)
		if err != nil {
			return ResolvedSide{}, err
		}
		side := cliout.DiffSideJSON{Ref: "local", Source: d.CanonicalQuery()}
		// The hash needs emit, which only a successful compile derives; omit it on a
		// compile failure rather than emit a misleading one.
		if compileErr == nil {
			side.Hash = d.Hash()
		}
		return ResolvedSide{JSON: side, Label: "local", Uncompiled: compileErr}, nil
	}
}

// localDescriptor builds the local side's descriptor from gaffer.toml. A source
// that doesn't compile is not fatal for a diff - the query still reads from
// source - so it returns the partial descriptor and the compile error as a
// non-fatal note (emit, and so the hash, is then unknown). The error return is
// reserved for genuinely unresolvable locals: absent from config, a config error,
// or an unreadable entry.
func localDescriptor(cfg *config.Config, root, name string) (desc deploy.Descriptor, compileErr, err error) {
	def := cfg.FindProjection(name)
	if def == nil {
		return deploy.Descriptor{}, nil, fmt.Errorf("%q is not in gaffer.toml", name)
	}
	if cfgErr := cfg.ProjectionConfigError(name); cfgErr != nil {
		return deploy.Descriptor{}, nil, cfgErr
	}
	source, err := engine.ReadSource(root, def.Entry)
	if err != nil {
		return deploy.Descriptor{}, nil, err
	}
	proj := engine.NewProjection(root, cfg, def, source)
	d, cErr := engine.LocalDescriptor(proj)
	if cErr != nil {
		return engine.PartialDescriptor(proj), cErr, nil
	}
	return d, nil, nil
}

// ShortRef abbreviates a content hash for a human label (git-style leading 7).
func ShortRef(hash string) string {
	if len(hash) >= 7 {
		return hash[:7]
	}
	return hash
}

// Build resolves both sides and diffs their source into a cliout.DiffJSON. It
// returns the resolved sides too, so a caller can note an uncompiled local side
// or label text output. A version-to-version diff carries no drift verdict - that
// is meaningful only for the default deployed↔local comparison.
func Build(ctx context.Context, r *remote.Client, cfg *config.Config, root, name string, left, right Ref) (cliout.DiffJSON, ResolvedSide, ResolvedSide, error) {
	ls, err := ResolveSide(ctx, r, cfg, root, name, left)
	if err != nil {
		return cliout.DiffJSON{}, ResolvedSide{}, ResolvedSide{}, err
	}
	rs, err := ResolveSide(ctx, r, cfg, root, name, right)
	if err != nil {
		return cliout.DiffJSON{}, ResolvedSide{}, ResolvedSide{}, err
	}
	lines := deploy.LineDiff(ls.JSON.Source, rs.JSON.Source)
	return cliout.DiffJSON{Name: name, Left: ls.JSON, Right: rs.JSON, Lines: lines}, ls, rs, nil
}
