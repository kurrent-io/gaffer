package cmd

import (
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

func ledgerEntry(tool, actor string) *remote.Ledger {
	return &remote.Ledger{Tool: tool, Actor: actor, Time: time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)}
}
