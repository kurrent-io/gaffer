package cmd

import (
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/stamp"
)

// toolLedger builds the tool metadata stamped on a create/update gaffer makes,
// from the env the command already resolved to connect. Kept as a wrapper so
// the CLI's version wiring stays in one place.
func toolLedger(env config.ResolvedEnv, operation, root string) remote.Ledger {
	return stamp.Ledger(env, operation, Version, root)
}
