package cmd

import (
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/stamp"
)

// toolLedger builds the tool metadata stamped on a create/update gaffer makes,
// resolving the live env from the command's flags first. An unresolvable env
// still stamps tool/version/operation/revision - only the actor is dropped.
func toolLedger(connection, env, operation string, cfg *config.Config, root string) remote.Ledger {
	resolved, err := resolveLiveEnv(connection, env, cfg)
	if err != nil {
		resolved = config.ResolvedEnv{}
	}
	return stamp.Ledger(resolved, operation, Version, root)
}
