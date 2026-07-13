package remote

import (
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
)

// Dial connects to a resolved environment and wraps the client for management
// RPCs, returning a cleanup to defer. It's the one place the status, diff,
// operate and deploy paths - the CLI commands, the MCP deploy tools, and the
// language server's status fetch - open a management connection, so the
// connect-and-wrap sequence isn't re-derived per caller.
//
// The engine's auth-invalidation handle is dropped: the only caller that reads
// it (the debug/dev live run) doesn't go through here. The connect error is
// returned verbatim so callers can still classify it (e.g. a
// target.AuthRequiredError into a sign-in prompt).
func Dial(projectRoot string, env config.ResolvedEnv) (*Client, func(), error) {
	client, _, err := engine.Connect(projectRoot, env)
	if err != nil {
		return nil, nil, err
	}
	return New(client), func() { _ = client.Close() }, nil
}
