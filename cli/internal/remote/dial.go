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
// The connect error is returned verbatim so callers can still classify it (e.g.
// a target.AuthRequiredError into a sign-in prompt). Most callers want this and
// can ignore the auth-invalidation handle; use DialWithAuth to observe it.
func Dial(projectRoot string, env config.ResolvedEnv) (*Client, func(), error) {
	client, _, cleanup, err := DialWithAuth(projectRoot, env)
	if err != nil {
		return nil, nil, err
	}
	return client, cleanup, nil
}

// DialWithAuth is Dial plus the engine's auth-invalidation handle, tripped when
// the IdP rejects the stored OAuth token (invalid_grant) on a read - a dead
// credential the connect itself can't see, because engine.Connect is lazy and
// the rejection only surfaces on the first RPC. A caller that offers a sign-in
// affordance (the language server's status fetch) checks Tripped() after a
// failed read to tell a dead credential apart from a generic failure. The
// handle is nil for a non-OAuth env.
func DialWithAuth(projectRoot string, env config.ResolvedEnv) (*Client, *engine.AuthInvalidation, func(), error) {
	client, authInv, err := engine.Connect(projectRoot, env)
	if err != nil {
		return nil, nil, nil, err
	}
	return New(client), authInv, func() { _ = client.Close() }, nil
}
