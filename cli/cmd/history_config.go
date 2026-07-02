package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// configChange is one tuning knob that moved between adjacent versions - what a
// `reconfigured` entry shows: which knob, and its before/after values.
type configChange struct {
	Label string
	From  string
	To    string
}

// configKnob renders one checkpoint/perf knob's value for display. Optional knobs
// read "default" at zero so a change reads "default -> 2000ms" rather than "0ms".
type configKnob struct {
	label string
	value func(remote.Config) string
}

var configKnobs = []configKnob{
	{"checkpoint after", func(c remote.Config) string { return msOrDefault(c.CheckpointAfterMs) }},
	{"handled threshold", func(c remote.Config) string { return strconv.Itoa(c.CheckpointHandledThreshold) }},
	{"unhandled bytes", func(c remote.Config) string { return strconv.Itoa(c.CheckpointUnhandledBytesThreshold) }},
	{"pending events", func(c remote.Config) string { return strconv.Itoa(c.PendingEventsThreshold) }},
	{"write batch", func(c remote.Config) string { return strconv.Itoa(c.MaxWriteBatchLength) }},
	{"writes in flight", func(c remote.Config) string { return intOrDefault(c.MaxAllowedWritesInFlight) }},
	{"exec timeout", func(c remote.Config) string { return msOrDefault(c.ProjectionExecutionTimeout) }},
	{"checkpoints", func(c remote.Config) string {
		if c.CheckpointsDisabled {
			return "disabled"
		}
		return "enabled"
	}},
}

func msOrDefault(ms int) string {
	if ms <= 0 {
		return "default"
	}
	return strconv.Itoa(ms) + "ms"
}

func intOrDefault(n int) string {
	if n <= 0 {
		return "default"
	}
	return strconv.Itoa(n)
}

// configChangesBetween lists the knobs that differ between two configs, oldest to
// newest, for a reconfigured entry's detail and summary.
func configChangesBetween(from, to remote.Config) []configChange {
	var out []configChange
	for _, k := range configKnobs {
		if a, b := k.value(from), k.value(to); a != b {
			out = append(out, configChange{Label: k.label, From: a, To: b})
		}
	}
	return out
}

// configSummary renders the changed knobs on one line, for the static timeline's
// provenance under a reconfigured entry.
func configSummary(changes []configChange) string {
	parts := make([]string, len(changes))
	for i, c := range changes {
		parts[i] = fmt.Sprintf("%s %s → %s", c.Label, c.From, c.To)
	}
	return strings.Join(parts, " · ")
}
