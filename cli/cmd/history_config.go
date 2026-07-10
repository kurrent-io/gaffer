package cmd

import (
	"fmt"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// configSummary renders the changed knobs on one line, for the static timeline's
// provenance under a reconfigured entry.
func configSummary(changes []remote.ConfigChange) string {
	parts := make([]string, len(changes))
	for i, c := range changes {
		parts[i] = fmt.Sprintf("%s %s → %s", c.Label, c.From, c.To)
	}
	return strings.Join(parts, dotSep)
}
