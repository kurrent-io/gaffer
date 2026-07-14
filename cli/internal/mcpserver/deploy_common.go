package mcpserver

import (
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// connectRemote resolves the env and dials it, returning the projection
// management client, the resolved env (for the config-drift check), and the
// cleanup to defer. The deploy tools connect per call: tool calls are
// sporadic and the server long-lived, so a held connection would just go
// stale between calls. The engine's auth-invalidation handle (for the editor's
// re-sign-in prompt) has no MCP UX, so remote.Dial drops it.
func (s *Server) connectRemote(cfg *config.Config, root, envName string) (*remote.Client, config.ResolvedEnv, func(), error) {
	env, err := mcpConnection(cfg, envName)
	if err != nil {
		return nil, config.ResolvedEnv{}, nil, err
	}
	r, cleanup, err := remote.Dial(root, env)
	if err != nil {
		return nil, config.ResolvedEnv{}, nil, err
	}
	return r, env, cleanup, nil
}
