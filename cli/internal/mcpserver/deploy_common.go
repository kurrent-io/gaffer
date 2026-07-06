package mcpserver

import (
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// connectRemote resolves the env and dials it, returning the projection
// management client, the resolved env (for the config-drift check), and the
// cleanup to defer. The deploy tools connect per call: tool calls are
// sporadic and the server long-lived, so a held connection would just go
// stale between calls.
func (s *Server) connectRemote(cfg *config.Config, root, envName string) (*remote.Client, config.ResolvedEnv, func(), error) {
	env, err := mcpConnection(cfg, envName)
	if err != nil {
		return nil, config.ResolvedEnv{}, nil, err
	}
	// The auth-invalidation handle drives the editor's re-sign-in prompt on a
	// debug run; the MCP server has no such UX, so it's dropped, like
	// connectToKurrentDB.
	client, _, err := engine.Connect(env.Connection, root, env.Name, env.OAuth, env.Cert)
	if err != nil {
		return nil, config.ResolvedEnv{}, nil, err
	}
	return remote.New(client), env, func() { _ = client.Close() }, nil
}
