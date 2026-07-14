package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// WriteStatus renders a single projection's status as a detail block: its
// runtime state (when deployed), how it compares to local, and - where a ledger
// is present - who last deployed it and from where.
func (tw *textWriter) WriteStatus(e drift.StatusEntry) {
	tw.heading(e.Name)
	if e.Runtime != nil {
		tw.detail("State", tw.runtimeStateStyle(e).Render(runtimeStateText(e)))
		tw.detail("Progress", progressText(e))
		if e.Runtime.Position != "" {
			tw.detail("Position", e.Runtime.Position)
		}
		if e.Runtime.State == remote.StateFaulted && e.Runtime.FaultReason != "" {
			tw.detail("Fault", tw.styles.errDetail.Render(e.Runtime.FaultReason))
		}
		if e.Runtime.State == remote.StateAborted {
			tw.detail("Resume", tw.styles.warning.Render("reprocesses from the last checkpoint (abort skipped the final one)"))
		}
	}
	tw.detail("Drift", tw.driftStyle(e.Comparison).Render(drift.Verdict(e.Comparison)))
	tw.writeLedgerProvenance(e.Comparison)
	if e.State == drift.Invalid && e.LocalErr != nil {
		tw.blank()
		tw.write("%s\n", tw.styles.errDetail.Render(e.LocalErr.Error()))
	}
}

// writeLedgerProvenance adds the deploy-provenance detail lines from the ledger -
// when, by which tool, who, and from what source revision. No-op without a ledger;
// an unreadable entry is flagged. Shared by the status detail block and gaffer diff.
func (tw *textWriter) writeLedgerProvenance(c drift.Comparison) {
	if at := c.LastDeployTime(); !at.IsZero() {
		tw.detail("Last deploy", at.Format("2006-01-02 15:04"))
	}
	if c.LedgerErr != nil {
		tw.detail("Deploy metadata", tw.styles.warning.Render("unreadable"))
		return
	}
	if c.Ledger == nil {
		return
	}
	via := c.Ledger.Tool
	if c.Ledger.ToolVersion != "" {
		via += " " + c.Ledger.ToolVersion
	}
	tw.detail("Deployed via", via)
	if c.Ledger.Actor != "" {
		tw.detail("Deployer", c.Ledger.Actor)
	}
	if c.Ledger.Revision != "" {
		tw.detail("Revision", shortRevision(c.Ledger.Revision))
	}
}

// shortRevision abbreviates a full git SHA to 12 chars for display (keeping any
// +changes dirty marker), and leaves a non-SHA revision (a custom --revision /
// GAFFER_REVISION value) untouched. The full value stays in --json.
func shortRevision(rev string) string {
	base, changes, dirty := strings.Cut(rev, "+")
	if len(base) != 40 {
		return rev
	}
	for _, c := range base {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return rev
		}
	}
	if dirty {
		return base[:12] + "+" + changes
	}
	return base[:12]
}

// WriteStatusTable renders all projections as a borderless aligned table. The
// cell text is plain; colour is applied per cell by the StyleFunc keying off the
// row's entry, so lipgloss's ANSI-aware width keeps the columns aligned.
func (tw *textWriter) WriteStatusTable(entries []drift.StatusEntry) {
	const pad = 3
	t := table.New().
		BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
		BorderColumn(false).BorderRow(false).BorderHeader(false).
		Headers("PROJECTION", "STATE", "PROGRESS", "LAST DEPLOY", "DEPLOYED VIA", "DRIFT").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return tw.styles.heading.PaddingRight(pad)
			}
			switch col {
			case 1:
				return tw.runtimeStateStyle(entries[row]).PaddingRight(pad)
			case 5:
				return tw.driftStyle(entries[row].Comparison).PaddingRight(pad)
			default:
				return tw.styles.emitted.PaddingRight(pad)
			}
		})
	for _, e := range entries {
		t.Row(e.Name, runtimeStateText(e), progressText(e), lastDeployText(e.Comparison), deployedViaText(e.Comparison), drift.Verdict(e.Comparison))
	}
	// Trim the column padding the last cell leaves as trailing whitespace (plain
	// in piped output; invisible inside the colour codes on a terminal).
	for line := range strings.SplitSeq(strings.TrimRight(t.String(), "\n"), "\n") {
		tw.write("%s\n", strings.TrimRight(line, " "))
	}
	for _, e := range entries {
		if e.State == drift.Drifted {
			tw.write("\n%s\n", tw.styles.pipe.Render("Drifted - run gaffer diff <projection> to see what changed."))
			break
		}
	}
}

func runtimeStateText(e drift.StatusEntry) string {
	if e.Runtime == nil {
		return "-"
	}
	return string(e.Runtime.State)
}

func progressText(e drift.StatusEntry) string {
	if e.Runtime == nil {
		return "-"
	}
	if e.Runtime.Progress < 0 {
		return "unknown"
	}
	return fmt.Sprintf("%.0f%%", e.Runtime.Progress)
}

func (tw *textWriter) runtimeStateStyle(e drift.StatusEntry) lipgloss.Style {
	if e.Runtime == nil {
		return tw.styles.emitted
	}
	switch e.Runtime.State {
	case remote.StateRunning:
		return tw.styles.added
	case remote.StateFaulted:
		return tw.styles.errStatus
	case remote.StateAborted:
		return tw.styles.warning
	default:
		return tw.styles.emitted
	}
}

// lastDeployText / deployedViaText fill the table's provenance columns. The date
// is the last-deploy/write time (from the ledger, else the deployed event), so it
// shows even for a projection with no tool metadata; "via" needs a tool entry. The
// table shows the date alone for scanning; the detail block adds the time.
func lastDeployText(c drift.Comparison) string {
	at := c.LastDeployTime()
	if at.IsZero() {
		return "-"
	}
	return at.Format("2006-01-02")
}

func deployedViaText(c drift.Comparison) string {
	if c.Ledger == nil {
		return "-"
	}
	return c.Ledger.Tool
}

// driftStyle colours the verdict by meaning: green healthy, red broken, orange
// wants-attention (drift, and orphan - your abandoned deploy), grey neutral (not
// deployed, and untracked - on the server but not yours). Keys off the comparison,
// since orphan and plain-untracked share the untracked state.
func (tw *textWriter) driftStyle(c drift.Comparison) lipgloss.Style {
	switch c.State {
	case drift.InSync:
		return tw.styles.added
	case drift.Invalid:
		return tw.styles.errStatus
	case drift.NotDeployed:
		return tw.styles.muted
	case drift.Untracked:
		if c.Owner() == drift.OwnerOrphan {
			return tw.styles.warning
		}
		return tw.styles.muted
	default: // drifted
		return tw.styles.warning
	}
}
