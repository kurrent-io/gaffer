package target

import (
	"fmt"

	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
)

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
	return cfg, nil
}
