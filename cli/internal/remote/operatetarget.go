package remote

import (
	"context"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

// OperateTarget names the write target and whether it gates as production,
// for confirms and guardrails. Defined once, shared by the CLI verbs and the
// MCP server, so one server never gates differently per surface. The
// identity comes from a bounded, best-effort $server-info read: the stream
// is advisory and unreadable on most DBs - absent, or ACL-restricted on a
// secured server - so any error falls back to the env's own name and
// production opt-in, and a real connection failure surfaces when the
// caller's own RPC runs. Trade-off: against an unreadable prod-declaring
// server the tier rests on the env config alone (re-permitting
// --no-validate unless the env opts in); the core never-act-unconfirmed
// guard still holds.
func (c *Client) OperateTarget(ctx context.Context, env config.ResolvedEnv, timeout time.Duration) (target string, prod bool) {
	sctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	info, _ := c.ServerInfo(sctx)
	return operateIdentity(info, env)
}

// operateIdentity derives the target name and production tier from the
// server's self-reported identity and the selected env. The name is the
// server's cluster name when it has one (authoritative), else the env name.
// The tier is the OR of the server's own declaration and the env's
// production flag, so config can add the tier but never remove a
// server-declared one - a local gaffer.toml can't disarm a production
// database.
func operateIdentity(info *ServerInfo, env config.ResolvedEnv) (target string, prod bool) {
	target = env.Name
	if info != nil && info.Name != "" {
		target = info.Name
	}
	return target, info.IsProduction() || env.Production
}
