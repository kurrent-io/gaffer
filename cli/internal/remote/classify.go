package remote

import (
	"strconv"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
)

// VersionKind is how a version came to be, derived from its tool metadata and how
// its definition compares to the older adjacent version. The metadata-less kinds
// (updated / enabled / disabled / reconfigured / rewritten) come from the descriptor
// diff. Whether a change is out-of-band is a separate, position-dependent signal -
// see OutOfBand - not baked into the kind.
type VersionKind string

const (
	KindDeploy        VersionKind = "deploy"
	KindRollback      VersionKind = "rollback"
	KindReset         VersionKind = "reset"
	KindRecreate      VersionKind = "recreate"
	KindUpdatedByTool VersionKind = "updated-by"   // + tool name
	KindUpdated       VersionKind = "updated"      // metadata-less, definition changed (author unknown)
	KindEnabled       VersionKind = "enabled"      // metadata-less, only the enabled flag flipped on
	KindDisabled      VersionKind = "disabled"     // metadata-less, only the enabled flag flipped off
	KindReconfigured  VersionKind = "reconfigured" // metadata-less, content + enabled unchanged, a config knob moved
	KindRewritten     VersionKind = "rewritten"    // metadata-less, content + enabled + config all unchanged (a no-op rewrite)
	KindCreated       VersionKind = "created"      // the first version, no gaffer metadata
	KindDeleted       VersionKind = "deleted"      // a tombstone (delete, or the first half of a recreate)
	KindUnreadable    VersionKind = "unreadable"   // the version's tool metadata wouldn't decode
)

// ClassifiedVersion is one version attributed against its older adjacent
// version: the raw version plus its full content hash and how it came to be.
type ClassifiedVersion struct {
	Version
	ContentHash   string            // full content hash of the version's definition; "" when it carries none
	Kind          VersionKind       // how this version came to be
	Tool          string            // the tool name, for KindUpdatedByTool
	Change        deploy.Comparison // dimensions that changed vs the older version, when the content changed
	HasChange     bool              // whether Change is meaningful
	ConfigChanges []ConfigChange    // knobs that moved vs the older version, when reconfigured
	// AfterGaffer is true when a gaffer-attributed write precedes this version in
	// the read window. It gates the out-of-band warning: a non-gaffer write only
	// reads as "changed outside gaffer" once gaffer has been managing the projection.
	AfterGaffer bool
}

// OutOfBand reports whether this version changed the projection
// outside gaffer after gaffer had been managing it: a non-gaffer write - a
// metadata-less edit (updated) or another tool's write (updated-by) - with a
// gaffer write earlier in the window. Before gaffer ever wrote, a non-gaffer
// change isn't a departure from gaffer, just an unattributed edit, so it isn't
// flagged. The out-of-band signal, matching deploy/status's attribution.
func (cv ClassifiedVersion) OutOfBand() bool {
	return cv.AfterGaffer && (cv.Kind == KindUpdated || cv.Kind == KindUpdatedByTool)
}

// StateChange reports whether this item is a lifecycle/state step (enable, disable,
// reset, delete, or a content-less config write) rather than a new content version.
// A state change carries no content identity of its own - it toggles the state of
// the content deployed before it - so a timeline leads it with the state word in
// place of a hash, and it isn't a rollback target.
func (cv ClassifiedVersion) StateChange() bool {
	switch cv.Kind {
	case KindEnabled, KindDisabled, KindReconfigured, KindRewritten, KindReset, KindDeleted:
		return true
	default: // deploy, rollback, updated, updatedByTool, created, unreadable
		return false
	}
}

// Enabled reports the projection's lifecycle state at this point in history, from
// the event's persisted state (absent on the wire means false, the canonical
// disabled value - see Definition.Enabled).
func (cv ClassifiedVersion) Enabled() bool {
	return cv.Definition != nil && cv.Definition.Enabled
}

// Classify attributes each raw version against the older adjacent version (the
// next one, since the slice is newest-first). It walks oldest -> newest so a
// latch can record whether a gaffer write precedes each version, which gates the
// out-of-band warning (see OutOfBand).
func Classify(versions []Version) []ClassifiedVersion {
	out := make([]ClassifiedVersion, len(versions))
	// A gaffer-attributed write seen among the versions strictly older than the
	// current one. Set as we pass each gaffer write (oldest first), so it reflects
	// only older versions when assigned to the current one.
	seenGaffer := false
	for i := len(versions) - 1; i >= 0; i-- {
		v := versions[i]
		cv := ClassifiedVersion{Version: v, AfterGaffer: seenGaffer}
		if v.Definition != nil {
			cv.ContentHash = v.Definition.Descriptor().Hash()
		}
		var prev *Version
		if i+1 < len(versions) {
			prev = &versions[i+1]
		}
		cv.Kind, cv.Tool, cv.Change, cv.HasChange = classifyVersion(v, prev)
		if cv.Kind == KindReconfigured {
			cv.ConfigChanges = configChangesBetween(prev.Definition.Config, v.Definition.Config)
		}
		out[i] = cv
		if v.Ledger != nil && v.Ledger.Tool == ToolName {
			seenGaffer = true
		}
	}
	return out
}

// classifyVersion attributes one version. A gaffer entry names its operation; a
// foreign entry is updated-by-tool; a metadata-less version is read from how its
// definition moved against prev (the older adjacent version), which may be nil for
// the oldest one in view - then only the genuine first version (v0) is a create,
// and an unattributable later no-op is reported neutrally as "rewritten". A flip
// of the enabled flag (absent means false) is an enable/disable; a moved config
// knob is a reconfigure.
func classifyVersion(v Version, prev *Version) (VersionKind, string, deploy.Comparison, bool) {
	if v.Deleted {
		return KindDeleted, "", deploy.Comparison{}, false
	}
	if v.MetaErr != nil {
		return KindUnreadable, "", deploy.Comparison{}, false
	}
	if v.Ledger != nil {
		if v.Ledger.Tool == ToolName {
			switch v.Ledger.Operation {
			case OpRollback:
				return KindRollback, "", deploy.Comparison{}, false
			case OpReset:
				return KindReset, "", deploy.Comparison{}, false
			case OpRecreate:
				return KindRecreate, "", deploy.Comparison{}, false
			default:
				return KindDeploy, "", deploy.Comparison{}, false
			}
		}
		return KindUpdatedByTool, v.Ledger.Tool, deploy.Comparison{}, false
	}
	if prev == nil || prev.Definition == nil || v.Definition == nil {
		if v.Number == 0 {
			return KindCreated, "", deploy.Comparison{}, false
		}
		return KindRewritten, "", deploy.Comparison{}, false
	}
	cmp := deploy.Compare(prev.Definition.Descriptor(), v.Definition.Descriptor())
	if !cmp.InSync() {
		return KindUpdated, "", cmp, true
	}
	if v.Definition.Enabled != prev.Definition.Enabled {
		if v.Definition.Enabled {
			return KindEnabled, "", deploy.Comparison{}, false
		}
		return KindDisabled, "", deploy.Comparison{}, false
	}
	if v.Definition.Config != prev.Definition.Config {
		return KindReconfigured, "", deploy.Comparison{}, false
	}
	return KindRewritten, "", deploy.Comparison{}, false
}

// ConfigChange is one tuning knob that moved between adjacent versions - what a
// `reconfigured` entry shows: which knob, and its before/after values.
type ConfigChange struct {
	Label string
	From  string
	To    string
}

// configKnob renders one checkpoint/perf knob's value for display. Optional knobs
// read "default" at zero so a change reads "default -> 2000ms" rather than "0ms".
type configKnob struct {
	label string
	value func(Config) string
}

var configKnobs = []configKnob{
	{"checkpoint after", func(c Config) string { return msOrDefault(c.CheckpointAfterMs) }},
	{"handled threshold", func(c Config) string { return strconv.Itoa(c.CheckpointHandledThreshold) }},
	{"unhandled bytes", func(c Config) string { return strconv.Itoa(c.CheckpointUnhandledBytesThreshold) }},
	{"pending events", func(c Config) string { return strconv.Itoa(c.PendingEventsThreshold) }},
	{"write batch", func(c Config) string { return strconv.Itoa(c.MaxWriteBatchLength) }},
	{"writes in flight", func(c Config) string { return intOrDefault(c.MaxAllowedWritesInFlight) }},
	{"exec timeout", func(c Config) string { return msOrDefault(c.ProjectionExecutionTimeout) }},
	{"checkpoints", func(c Config) string {
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
func configChangesBetween(from, to Config) []ConfigChange {
	var out []ConfigChange
	for _, k := range configKnobs {
		if a, b := k.value(from), k.value(to); a != b {
			out = append(out, ConfigChange{Label: k.label, From: a, To: b})
		}
	}
	return out
}
