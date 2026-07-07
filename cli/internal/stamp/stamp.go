// Package stamp builds the tool-metadata ledger gaffer records on the
// projection writes it makes (deploy, rollback, recreate), shared by the
// CLI commands and the MCP server's write tools so attribution can't
// drift between surfaces.
package stamp

import (
	"os"
	"os/exec"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// Ledger builds the tool metadata stamped on a create/update gaffer makes:
// always tool/version and the given operation, plus best-effort revision
// (source provenance) and actor (the identity gaffer connects as for the
// resolved env). A zero env yields an empty actor; empty best-effort fields
// are dropped on the wire by Ledger.metadata.
func Ledger(env config.ResolvedEnv, operation, version, root string) remote.Ledger {
	return remote.Ledger{
		Tool:        remote.ToolName,
		ToolVersion: version,
		Operation:   operation,
		Revision:    resolveRevision(root),
		Actor:       resolveActor(env, root),
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
// for the resolved env (basic-auth username or OAuth client_id), or "" (omitted)
// when there's no user - an anonymous / cert-only connection, or no resolved env.
// GAFFER_ACTOR overrides it - a CI-only knob for the pipeline/human identity,
// since the connection there is usually a service account. Client-asserted
// attribution, not a verified claim.
func resolveActor(env config.ResolvedEnv, root string) string {
	if a := os.Getenv("GAFFER_ACTOR"); a != "" {
		return a
	}
	if env.Connection == "" {
		return ""
	}
	return engine.Principal(root, env)
}
