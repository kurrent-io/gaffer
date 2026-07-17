package target

import (
	"fmt"
	"strings"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
)

// cliDiscoverAttempts is gaffer's node-discovery attempt default, lower than the
// client library's 10. Ten attempts take ~7s to give up against an unreachable
// endpoint - too slow for an interactive CLI or editor action, which should
// report "can't reach it" promptly. A reachable endpoint connects on the first
// attempt, so this only shortens the unreachable case.
const cliDiscoverAttempts = 2

// ParseConnection parses a KurrentDB connection string with an error that is
// safe to surface: the parser echoes its input, which for a malformed
// connection string can include an inline password, so the message carries
// only redacted/scrubbed forms. The single parse path for anything that
// reports a connection-string failure to a user (engine.Connect, the OAuth
// host binding), so the redaction guarantees can't drift between call sites.
func ParseConnection(connection string) (*kurrentdb.Configuration, error) {
	cfg, err := kurrentdb.ParseConnectionString(connection)
	if err != nil {
		return nil, fmt.Errorf("invalid connection string %s: %s", RedactConnection(connection), ScrubConnection(err.Error(), connection))
	}
	// Fail fast on an unreachable endpoint unless the user set the attempt count
	// themselves. ParseConnectionString has already applied the library default
	// (10) when the string omits it and doesn't report which happened, so detect
	// an explicit value from the raw string.
	if !strings.Contains(strings.ToLower(connection), "maxdiscoverattempts") {
		cfg.MaxDiscoverAttempts = cliDiscoverAttempts
	}
	return cfg, nil
}
