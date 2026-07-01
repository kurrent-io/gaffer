package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// historyDefaultLimit bounds the non-interactive (piped / --json) read. The
// interactive picker ignores it and pages from the head instead.
const historyDefaultLimit = 100

type historyOpts struct {
	Env        string
	Connection string
	JSON       bool
	Limit      int
	All        bool
}

func newHistoryCmd() *cobra.Command {
	var opts historyOpts
	cmd := &cobra.Command{
		Use:   "history <projection>",
		Short: "Show a deployed projection's history",
		Long: "Show the history of a deployed projection: every operation on it, newest\n" +
			"first, with who made it and how.\n\n" +
			"Each entry is one write to the projection on the server. An entry carrying\n" +
			"gaffer metadata shows the operation (deploy, rollback, reset), the actor, and the\n" +
			"source revision. An entry with no gaffer metadata is attributed by what changed:\n" +
			"edited externally when the definition was changed outside gaffer, changed by\n" +
			"another tool when it carries that tool's metadata, enabled/disabled for a\n" +
			"lifecycle change, or reconfigured when a checkpoint setting moved. A content hash\n" +
			"identifies the deployed definition, so a reverted definition is recognisable at a\n" +
			"glance.\n\n" +
			"On a terminal this opens an interactive timeline, the selected entry's detail\n" +
			"alongside; move with the arrow keys (g/G to jump to the ends, q to quit), and a\n" +
			"reverted definition is drawn as a branch back to the deploy it matched. Piped or\n" +
			"with --json it prints the latest entries (--limit, default 100, or --all).\n" +
			"Against a server without gaffer metadata it degrades to the history with\n" +
			"timestamps and content hashes only.",
		Example: "  gaffer history order-count\n" +
			"  gaffer history order-count --env staging\n" +
			"  gaffer history order-count --json",
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHistory(cmd, args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment from gaffer.toml")
	cmd.Flags().StringVar(&opts.Connection, "connection", "", "KurrentDB connection string (overrides --env)")
	cmd.Flags().BoolVar(&opts.JSON, "json", false, "Output as JSON")
	cmd.Flags().IntVar(&opts.Limit, "limit", historyDefaultLimit, "Maximum entries to show (piped / --json only)")
	cmd.Flags().BoolVar(&opts.All, "all", false, "Show all entries, ignoring --limit (piped / --json only)")
	return cmd
}

func runHistory(cmd *cobra.Command, name string, opts historyOpts) error {
	conn, err := connectEnv(opts.Connection, opts.Env)
	if err != nil {
		return err
	}
	defer conn.cleanup()

	// On a terminal (and not --json) the interactive timeline pages versions in
	// itself, so it owns the read; the static and JSON paths read a bounded window
	// here. resolveLiveEnv re-derives what connectEnv already resolved, only for
	// the footer's env/target labels - it can't fail now that the connect succeeded.
	if !opts.JSON && interactiveWriter(cmd.OutOrStdout()) {
		resolved, _ := resolveLiveEnv(opts.Connection, opts.Env, conn.cfg)
		return runHistoryTUI(cmd, conn.r, name, resolved.Name, redactConnection(resolved.Connection))
	}

	// remote calls block until their context deadline if the projections
	// subsystem doesn't respond, so bound the read rather than hang the command.
	ctx, cancel := context.WithTimeout(cmd.Context(), projectionRPCTimeout)
	defer cancel()

	limit := opts.Limit
	if opts.All {
		limit = 0 // ReadHistory reads up to its hard cap
	}
	// Read one extra version beyond the display limit: the oldest shown row is
	// classified against its predecessor, so without the extra the bottom row of a
	// bounded listing would always fall back to "rewritten". The extra is the
	// baseline only, dropped before output.
	readLimit := limit
	if readLimit > 0 {
		readLimit++
	}
	versions, total, err := conn.r.ReadHistory(ctx, name, -1, readLimit)
	if err != nil {
		if errors.Is(err, remote.ErrNotFound) {
			return fmt.Errorf("%w: %q is not deployed on the server", remote.ErrNotFound, name)
		}
		return err
	}
	hist := classifyHistory(versions)
	if limit > 0 && len(hist) > limit {
		hist = hist[:limit]
	}

	if opts.JSON {
		return renderHistoryJSON(cmd.OutOrStdout(), hist)
	}
	newTextWriter(cmd.OutOrStdout(), cmd.ErrOrStderr()).WriteHistory(name, hist, total)
	return nil
}

// redactConnection reduces a connection string to its host(s) for display,
// dropping the scheme, any credentials, and query options so the footer never
// prints a password. Falls back to a generic label if it can't be parsed.
func redactConnection(conn string) string {
	if conn == "" {
		return ""
	}
	u, err := url.Parse(conn)
	if err != nil || u.Host == "" {
		// Not a URL we can pick apart (e.g. a bare host list); show nothing
		// sensitive rather than risk echoing embedded credentials.
		return "the target"
	}
	return u.Host
}

// versionKind is how a version came to be, derived from its tool metadata and how
// its definition compares to the older adjacent version. The metadata-less kinds
// (edited-externally / enabled / disabled / changed) come from the descriptor
// diff, so a row reads "edited externally" exactly when gaffer diff would call the
// two versions drifted - one source of truth with status and diff.
type versionKind string

const (
	kindDeploy           versionKind = "deploy"
	kindRollback         versionKind = "rollback"
	kindReset            versionKind = "reset"
	kindChangedByTool    versionKind = "changed by"        // + tool name
	kindEditedExternally versionKind = "edited externally" // metadata-less, definition changed
	kindEnabled          versionKind = "enabled"           // metadata-less, only the enabled flag flipped on
	kindDisabled         versionKind = "disabled"          // metadata-less, only the enabled flag flipped off
	kindReconfigured     versionKind = "reconfigured"      // metadata-less, content + enabled unchanged, a config knob moved
	kindRewritten        versionKind = "rewritten"         // metadata-less, content + enabled + config all unchanged (a no-op rewrite)
	kindCreated          versionKind = "created"           // the first version, no gaffer metadata
	kindDeleted          versionKind = "deleted"           // a tombstone (delete, or the first half of a recreate)
	kindUnreadable       versionKind = "unreadable"        // the version's tool metadata wouldn't decode
)

// historyVersion is one version prepared for display: the raw version plus its
// short content hash and how it was classified against the older adjacent version.
type historyVersion struct {
	remote.Version
	Hash          string            // short content hash of the definition at this version
	Kind          versionKind       // how this version came to be
	Tool          string            // the tool name, for kindChangedByTool
	Change        deploy.Comparison // dimensions that changed vs the older version, when edited externally
	HasChange     bool              // whether Change is meaningful
	ConfigChanges []configChange    // knobs that moved vs the older version, when reconfigured
}

// external reports whether this version changed the definition outside gaffer -
// a metadata-less edit (edited externally) or another tool's write (changed by).
// The out-of-band flag the ticket calls for, matching deploy's externallyChanged.
func (hv historyVersion) external() bool {
	return hv.Kind == kindEditedExternally || hv.Kind == kindChangedByTool
}

// stateChange reports whether this item is a lifecycle/state step (enable, disable,
// reset, delete, or a content-less config write) rather than a new content version.
// A state change carries no content identity of its own - it toggles the state of
// the content deployed before it - so the timeline leads it with the state word in
// place of a hash, and it isn't a rollback target.
func (hv historyVersion) stateChange() bool {
	switch hv.Kind {
	case kindEnabled, kindDisabled, kindReconfigured, kindRewritten, kindReset, kindDeleted:
		return true
	default: // deploy, rollback, editedExternally, changedByTool, created, unreadable
		return false
	}
}

// enabled reports the projection's lifecycle state at this point in history, from
// the event's persisted state (absent on the wire means false, the canonical
// disabled value - see remote.Definition.Enabled).
func (hv historyVersion) enabled() bool {
	return hv.Definition != nil && hv.Definition.Enabled
}

// operationLabel is the operation to show for a content version: its stamped
// operation (deploy / rollback / reset) when present, else the implicit one - a
// create, or an "updated" for an unstamped external or foreign edit (the operation
// is implicit from what happened, even with no metadata to name it).
func (hv historyVersion) operationLabel() string {
	if hv.Ledger != nil && hv.Ledger.Operation != "" {
		return hv.Ledger.Operation
	}
	switch hv.Kind {
	case kindCreated:
		return "created"
	case kindEditedExternally, kindChangedByTool:
		return "updated"
	default:
		return ""
	}
}

// eventLabel is the human label for the row's kind, with the tool name folded in
// for a foreign write.
func (hv historyVersion) eventLabel() string {
	switch hv.Kind {
	case kindChangedByTool:
		if hv.Tool != "" {
			return "changed by " + hv.Tool
		}
		return "changed externally"
	case kindUnreadable:
		return "unreadable metadata"
	default:
		return string(hv.Kind)
	}
}

// classifyHistory prepares each raw version for display, classifying it against
// the older adjacent version (the next one, since the slice is newest-first).
func classifyHistory(versions []remote.Version) []historyVersion {
	out := make([]historyVersion, len(versions))
	for i := range versions {
		v := versions[i]
		hv := historyVersion{Version: v}
		if v.Definition != nil {
			hv.Hash = shortHash(v.Definition.Descriptor().Hash())
		}
		var prev *remote.Version
		if i+1 < len(versions) {
			prev = &versions[i+1]
		}
		hv.Kind, hv.Tool, hv.Change, hv.HasChange = classifyVersion(v, prev)
		if hv.Kind == kindReconfigured {
			hv.ConfigChanges = configChangesBetween(prev.Definition.Config, v.Definition.Config)
		}
		out[i] = hv
	}
	return out
}

// classifyVersion attributes one version. A gaffer entry names its operation; a
// foreign entry is changed-by-tool; a metadata-less version is read from how its
// definition moved against prev (the older adjacent version), which may be nil for
// the oldest one in view - then only the genuine first version (v0) is a create,
// and an unattributable later no-op is reported neutrally as "rewritten". A flip
// of the enabled flag (absent means false) is an enable/disable; a moved config
// knob is a reconfigure.
func classifyVersion(v remote.Version, prev *remote.Version) (versionKind, string, deploy.Comparison, bool) {
	if v.Deleted {
		return kindDeleted, "", deploy.Comparison{}, false
	}
	if v.MetaErr != nil {
		return kindUnreadable, "", deploy.Comparison{}, false
	}
	if v.Ledger != nil {
		if v.Ledger.Tool == remote.ToolName {
			switch v.Ledger.Operation {
			case remote.OpRollback:
				return kindRollback, "", deploy.Comparison{}, false
			case remote.OpReset:
				return kindReset, "", deploy.Comparison{}, false
			default:
				return kindDeploy, "", deploy.Comparison{}, false
			}
		}
		return kindChangedByTool, v.Ledger.Tool, deploy.Comparison{}, false
	}
	if prev == nil || prev.Definition == nil || v.Definition == nil {
		if v.Number == 0 {
			return kindCreated, "", deploy.Comparison{}, false
		}
		return kindRewritten, "", deploy.Comparison{}, false
	}
	cmp := deploy.Compare(prev.Definition.Descriptor(), v.Definition.Descriptor())
	if !cmp.InSync() {
		return kindEditedExternally, "", cmp, true
	}
	if v.Definition.Enabled != prev.Definition.Enabled {
		if v.Definition.Enabled {
			return kindEnabled, "", deploy.Comparison{}, false
		}
		return kindDisabled, "", deploy.Comparison{}, false
	}
	if v.Definition.Config != prev.Definition.Config {
		return kindReconfigured, "", deploy.Comparison{}, false
	}
	return kindRewritten, "", deploy.Comparison{}, false
}

// shortHash abbreviates a content hash to the leading 7 chars (git-style), the
// version id shown on each row. The full hash stays in --json.
func shortHash(h string) string {
	if len(h) >= 7 {
		return h[:7]
	}
	return h
}

// historyJSON is the --json shape for one version. external is the out-of-band
// flag (edited outside gaffer); kind is the classification; the tool fields are
// present only when the version carried metadata.
type historyJSON struct {
	Version       int64              `json:"version"`
	Time          string             `json:"time"`
	ContentHash   string             `json:"contentHash"`
	Kind          string             `json:"kind"`
	Enabled       bool               `json:"enabled"`
	External      bool               `json:"external"`
	StateChange   bool               `json:"stateChange,omitempty"`
	Deleted       bool               `json:"deleted,omitempty"`
	Tool          string             `json:"tool,omitempty"`
	ToolVersion   string             `json:"toolVersion,omitempty"`
	Operation     string             `json:"operation,omitempty"`
	Actor         string             `json:"actor,omitempty"`
	Revision      string             `json:"revision,omitempty"`
	ConfigChanges []configChangeJSON `json:"configChanges,omitempty"`
}

type configChangeJSON struct {
	Knob string `json:"knob"`
	From string `json:"from"`
	To   string `json:"to"`
}

func renderHistoryJSON(w io.Writer, versions []historyVersion) error {
	out := make([]historyJSON, 0, len(versions))
	for _, hv := range versions {
		j := historyJSON{
			Version:     hv.Number,
			ContentHash: "",
			Kind:        string(hv.Kind),
			Enabled:     hv.enabled(),
			External:    hv.external(),
			StateChange: hv.stateChange(),
			Deleted:     hv.Deleted,
		}
		if hv.Definition != nil {
			j.ContentHash = hv.Definition.Descriptor().Hash()
			if !hv.Definition.Time.IsZero() {
				j.Time = hv.Definition.Time.Format(time.RFC3339)
			}
		}
		if hv.Ledger != nil {
			j.Tool = hv.Ledger.Tool
			j.ToolVersion = hv.Ledger.ToolVersion
			j.Operation = hv.Ledger.Operation
			j.Actor = hv.Ledger.Actor
			j.Revision = hv.Ledger.Revision
		}
		for _, cc := range hv.ConfigChanges {
			j.ConfigChanges = append(j.ConfigChanges, configChangeJSON{Knob: cc.Label, From: cc.From, To: cc.To})
		}
		out = append(out, j)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
