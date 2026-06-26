package cmd

import (
	"os"
	"os/exec"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// deployLedger builds the tool metadata stamped on every create/update this deploy
// makes: always tool/version/operation, plus best-effort revision (source
// provenance) and actor (the identity gaffer connects as). Empty best-effort fields
// are dropped on the wire by Ledger.metadata.
func deployLedger(opts deployOpts, cfg *config.Config, root string) remote.Ledger {
	return remote.Ledger{
		Tool:        remote.ToolName,
		ToolVersion: Version,
		Operation:   remote.OpDeploy,
		Revision:    resolveRevision(root),
		Actor:       resolveActor(opts, cfg, root),
	}
}

// resolveRevision returns the source revision to record: the project's git HEAD,
// or "" when it isn't a git checkout. GAFFER_REVISION overrides it - a CI-only knob
// for recording the canonical commit when the checkout's HEAD isn't it (e.g. a PR
// build's synthetic merge commit).
func resolveRevision(root string) string {
	if r := os.Getenv("GAFFER_REVISION"); r != "" {
		return r
	}
	return gitRevision(root)
}

// gitRevision is the git HEAD commit of dir, suffixed +changes when the working
// tree has uncommitted changes (tracked or untracked - an untracked projection
// file means deploying source that isn't in the commit). Best-effort: "" if dir
// isn't a git repo, git isn't installed, or HEAD is unborn.
func gitRevision(dir string) string {
	head, err := git(dir, "rev-parse", "HEAD")
	if err != nil || head == "" {
		return ""
	}
	if dirty, err := git(dir, "status", "--porcelain"); err == nil && dirty != "" {
		return head + "+changes"
	}
	return head
}

func git(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output() //nolint:gosec // git with controlled args
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// resolveActor returns the identity to record: the principal gaffer connects as
// for the target env (basic-auth username or OAuth client_id), or "" (omitted) when
// there's no user - an anonymous / cert-only connection, or the env can't be
// resolved. GAFFER_ACTOR overrides it - a CI-only knob for the pipeline/human
// identity, since the connection there is usually a service account. Client-asserted
// attribution, not a verified claim.
func resolveActor(opts deployOpts, cfg *config.Config, root string) string {
	if a := os.Getenv("GAFFER_ACTOR"); a != "" {
		return a
	}
	resolved, err := resolveLiveEnv(opts.Connection, opts.Env, cfg)
	if err != nil {
		return ""
	}
	return engine.Principal(resolved.Connection, root, resolved.Name, resolved.OAuth)
}
