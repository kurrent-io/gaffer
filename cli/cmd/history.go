package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
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
			"gaffer metadata shows the operation (deploy, rollback, reset, recreate), the\n" +
			"actor, and the source revision. A recreate shows as a single entry with its\n" +
			"disable and delete steps folded in; --json keeps every write as its own entry.\n" +
			"An entry with no gaffer metadata is attributed by what changed:\n" +
			"edited externally when the definition was changed outside gaffer, changed by\n" +
			"another tool when it carries that tool's metadata, enabled/disabled for a\n" +
			"lifecycle change, or reconfigured when a checkpoint setting moved. A content hash\n" +
			"identifies the deployed definition, so a reverted definition is recognisable at a\n" +
			"glance.\n\n" +
			"On a terminal this opens an interactive timeline, the selected entry's detail\n" +
			"alongside; move with the arrow keys (g/G to jump to the ends, q to quit), and a\n" +
			"reverted definition is drawn as a branch back to the deploy it matched. Press d\n" +
			"to see the change an entry introduced as a source diff against the version\n" +
			"before it; the arrows keep working under the diff, walking the definition's\n" +
			"evolution entry by entry. Press r to roll back to the selected version: a\n" +
			"confirm shows what would change (see gaffer rollback), and an applied rollback\n" +
			"reloads the timeline with the new entry on top. Piped or with --json it prints the latest entries\n" +
			"(--limit, default 100, or --all). Against a server without gaffer metadata it\n" +
			"degrades to the history with timestamps and content hashes only.",
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
	// here.
	if !opts.JSON && interactiveWriter(cmd.OutOrStdout()) {
		// The ledger a timeline-applied rollback stamps; resolved once here, so
		// the modal's confirm doesn't pay the actor/revision lookups per apply.
		ledger := toolLedger(conn.env, remote.OpRollback, conn.root)
		return runHistoryTUI(cmd, conn.r, name, conn.env.Name, redactConnection(conn.env.Connection), ledger)
	}

	// remote calls block until their context deadline if the projections
	// subsystem doesn't respond, so bound the read rather than hang the command.
	ctx, cancel := context.WithTimeout(cmd.Context(), projectionRPCTimeout)
	defer cancel()

	limit := opts.Limit
	if opts.All {
		limit = 0 // ReadHistory reads up to its hard cap
	}
	// The baseline over-read below asks for limit+1, and ReadHistory clamps
	// anything above its hard cap - a --limit at the cap would silently lose
	// the baseline and misclassify the listing's oldest row as "rewritten".
	if limit >= remote.HistoryHardCap {
		limit = remote.HistoryHardCap - 1
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

	if opts.JSON {
		// --json stays uncollapsed - one object per stream write - so its limit
		// counts raw entries.
		if limit > 0 && len(hist) > limit {
			hist = hist[:limit]
		}
		return renderHistoryJSON(cmd.OutOrStdout(), hist)
	}
	newTextWriter(cmd.OutOrStdout(), cmd.ErrOrStderr()).WriteHistory(name, historyRows(hist, limit), total)
	return nil
}

// historyRows prepares the human timeline under --limit: fold first, then trim,
// so a recreate's bookends never count toward the limit only to be folded away.
// The window may carry one extra row beyond the limit - the classification
// baseline readLimit adds. Folding runs over the full window so a baseline that
// is itself a bookend folds into the last entry; a baseline that survives is
// dropped, since it exists only to classify the row above it and, metadata-less,
// would display as a bogus "rewritten" with nothing older in view. A window with
// folds can still show fewer than limit rows (the read is bounded); the
// Showing-N-of-M tail points at --limit/--all for the rest.
func historyRows(hist []historyVersion, limit int) []historyVersion {
	rows := collapseHistory(hist)
	if limit > 0 && len(hist) > limit {
		if last := rows[len(rows)-1]; last.Number == hist[len(hist)-1].Number {
			rows = rows[:len(rows)-1]
		}
	}
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
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

// historyVersion is one classified version prepared for display: the attributed
// version plus its short content hash and, in the collapsed human timeline, the
// bookend writes folded into it.
type historyVersion struct {
	remote.ClassifiedVersion
	Hash     string           // short content hash, for display
	Absorbed []historyVersion // a recreate's folded bookend writes (tombstone, then the disable)
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
	case remote.KindCreated:
		return "created"
	case remote.KindEditedExternally, remote.KindChangedByTool:
		return "updated"
	default:
		return ""
	}
}

// eventLabel is the human label for the row's kind, with the tool name folded in
// for a foreign write. The hyphenated wire tokens (the --json / MCP values)
// read as words here.
func (hv historyVersion) eventLabel() string {
	switch hv.Kind {
	case remote.KindChangedByTool:
		if hv.Tool != "" {
			return "changed by " + hv.Tool
		}
		return "changed externally"
	case remote.KindEditedExternally:
		return "edited externally"
	case remote.KindUnreadable:
		return "unreadable metadata"
	default:
		return string(hv.Kind)
	}
}

// classifyHistory prepares each raw version for display: remote.Classify
// attributes it against the older adjacent version, and the short display
// hash is derived from the full content hash.
func classifyHistory(versions []remote.Version) []historyVersion {
	classified := remote.Classify(versions)
	out := make([]historyVersion, len(classified))
	for i, cv := range classified {
		out[i] = historyVersion{ClassifiedVersion: cv, Hash: shortHash(cv.ContentHash)}
	}
	return out
}

// collapseHistory folds a recreate's bookend writes into its stamped create row
// for the human timeline: the delete tombstone directly below it, then the
// disable write directly below that (a disabled flip, or a rewritten no-op when
// the projection was already disabled). Recreate's steps write consecutively, so
// only the exact adjacent pattern folds - anything interleaved stays a visible
// row - and a recreate whose bookends sit on an unloaded page folds them when
// they arrive. --json stays uncollapsed: every stream write remains an entry.
func collapseHistory(versions []historyVersion) []historyVersion {
	out := make([]historyVersion, 0, len(versions))
	for i := 0; i < len(versions); i++ {
		hv := versions[i]
		if hv.Kind == remote.KindRecreate && i+1 < len(versions) && versions[i+1].Kind == remote.KindDeleted {
			hv.Absorbed = append(hv.Absorbed, versions[i+1])
			i++
			if i+1 < len(versions) && (versions[i+1].Kind == remote.KindDisabled || versions[i+1].Kind == remote.KindRewritten) {
				hv.Absorbed = append(hv.Absorbed, versions[i+1])
				i++
			}
		}
		out = append(out, hv)
	}
	return out
}

// absorbedCount is how many raw entries the collapsed view folded away, for
// adjusting a total that counts stream writes down to displayed rows.
func absorbedCount(versions []historyVersion) int64 {
	var n int64
	for _, hv := range versions {
		n += int64(len(hv.Absorbed))
	}
	return n
}

// shortHash abbreviates a content hash to the leading 7 chars (git-style), the
// version id shown on each row. The full hash stays in --json.
func shortHash(h string) string {
	if len(h) >= 7 {
		return h[:7]
	}
	return h
}

func renderHistoryJSON(w io.Writer, versions []historyVersion) error {
	classified := make([]remote.ClassifiedVersion, len(versions))
	for i, hv := range versions {
		classified[i] = hv.ClassifiedVersion
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(cliout.BuildHistoryJSON(classified))
}
