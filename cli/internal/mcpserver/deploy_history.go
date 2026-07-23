package mcpserver

import (
	"context"
	"errors"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// deployHistoryDefaultLimit bounds a tool call that names no limit, matching
// the CLI's non-interactive default.
const deployHistoryDefaultLimit = 100

var deployHistoryTool = &mcp.Tool{
	Name: "deploy_history",
	Description: "Read a deployed projection's audit history from a KurrentDB environment: " +
		"every write, newest first, one entry per stream write (uncollapsed), with actor, " +
		"tool, timestamps, and content hashes. Mirrors `gaffer history --json`; the " +
		"response echoes the resolved env. kind is one of: deploy, rollback, reset, " +
		"recreate, updated-by (another tool - see the tool field), updated (metadata-less " +
		"edit), enabled, disabled, reconfigured, rewritten (a no-op rewrite), created, " +
		"deleted (a tombstone), unreadable. outOfBand flags a non-gaffer write " +
		"made after gaffer began managing the projection. A contentHash identifies the deployed definition, so " +
		"reverts are recognisable and rollback targets can be picked by hash. Page older " +
		"entries by passing the previous page's oldest `version` as `before`; `total` " +
		"(the projection's total write count) is present only on the first (head) page " +
		"and omitted on paged calls.",
	Annotations: readOnlyHints(),
}

type deployHistoryInput struct {
	Name  string `json:"name" jsonschema:"Projection name."`
	Env   string `json:"env,omitempty" jsonschema:"Environment from gaffer.toml ([env.<name>]); omit for the default env."`
	Limit int    `json:"limit,omitempty" jsonschema:"Maximum entries to return. Defaults to 100."`
	// A pointer so an explicit 0 survives: nothing is older than version 0,
	// so before=0 is the empty terminal page, not a head read - an agent
	// mechanically passing the previous page's oldest version terminates
	// instead of looping back to page one.
	Before *int64 `json:"before,omitempty" jsonschema:"Return entries strictly older than this version number (paging): pass the previous page's oldest version. Version 0 is the oldest, so a page containing it is the last. Omit to start from the newest."`
}

func (s *Server) handleDeployHistory(ctx context.Context, _ *mcp.CallToolRequest, in deployHistoryInput) (*mcp.CallToolResult, any, error) {
	cfg, root, r := s.requireProject()
	if r != nil {
		return r, nil, nil
	}
	if in.Name == "" {
		return toolError("name required: pass the projection to read history for"), nil, nil
	}

	client, env, cleanup, err := s.connectRemote(cfg, root, in.Env)
	if err != nil {
		return toolError("%v", err), nil, nil
	}
	defer cleanup()

	limit := in.Limit
	if limit <= 0 {
		limit = deployHistoryDefaultLimit
	}
	// The baseline over-read below asks for limit+1, and ReadHistory clamps
	// anything above its hard cap - a limit at the cap would silently lose the
	// baseline and misclassify the page's oldest entry as "rewritten". Keep
	// the display limit below the cap so the baseline always fits.
	if limit >= remote.HistoryHardCap {
		limit = remote.HistoryHardCap - 1
	}
	before := int64(-1) // head read
	if in.Before != nil {
		before = *in.Before
	}

	// Management calls block until their deadline if the projections subsystem
	// is still starting, so bound the read rather than hang the tool call.
	rctx, cancel := context.WithTimeout(ctx, deploy.RPCTimeout)
	defer cancel()

	// Read one extra version beyond the limit: the oldest returned entry is
	// classified against its predecessor, so without it the last entry of a
	// bounded page would always fall back to "rewritten". The extra is the
	// baseline only, dropped before output.
	versions, total, err := client.ReadHistory(rctx, in.Name, before, limit+1)
	if err != nil {
		if errors.Is(err, remote.ErrNotFound) {
			return toolError("projection %q is not deployed on the server", in.Name), nil, nil
		}
		return toolError("%v", err), nil, nil
	}
	classified := remote.Classify(versions)
	if len(classified) > limit {
		classified = classified[:limit]
	}

	result := map[string]any{
		"env":      env.Name,
		"versions": cliout.BuildHistoryJSON(classified),
	}
	// The stream's total write count is known only on a head read; a paged
	// read reports -1, which would just be noise.
	if total >= 0 {
		result["total"] = total
	}
	return toolResult(result), nil, nil
}
