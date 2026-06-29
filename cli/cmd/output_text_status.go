package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// WriteStatus renders a single projection's status as a detail block: its
// runtime state (when deployed), how it compares to local, and - where a ledger
// is present - who last deployed it and from where.
func (tw *textWriter) WriteStatus(e statusEntry) {
	tw.heading(e.Name)
	if e.runtime != nil {
		tw.detail("State", tw.runtimeStateStyle(e).Render(runtimeStateText(e)))
		tw.detail("Progress", progressText(e))
		if e.runtime.Position != "" {
			tw.detail("Position", e.runtime.Position)
		}
		if e.runtime.State == remote.StateFaulted && e.runtime.FaultReason != "" {
			tw.detail("Fault", tw.styles.errDetail.Render(e.runtime.FaultReason))
		}
	}
	tw.detail("Drift", tw.driftStyle(e.comparison).Render(driftVerdict(e.comparison)))
	tw.writeLedgerProvenance(e.comparison)
	if e.State == driftInvalid && e.LocalErr != nil {
		tw.blank()
		tw.write("%s\n", tw.styles.errDetail.Render(e.LocalErr.Error()))
	}
}

// driftVerdict is the terse comparison verdict shared by the status table, the
// status detail block and gaffer diff. Drift is refined by attribution (local
// ahead / changed externally) and an untracked projection by ownership (orphan vs
// plain untracked - the DEPLOYED VIA column / provenance names the tool behind it).
func driftVerdict(c comparison) string {
	switch c.State {
	case driftUntracked:
		if c.owner() == ownerOrphan {
			return "orphan"
		}
		return "untracked"
	case driftDrifted:
		switch c.attribution() {
		case attrLocalAhead:
			return "local ahead"
		case attrChangedByTool, attrChangedServer:
			return "changed externally"
		default:
			return "drifted"
		}
	default:
		return driftText(c.State)
	}
}

// writeLedgerProvenance adds the deploy-provenance detail lines from the ledger -
// when, by which tool, who, and from what source revision. No-op without a ledger;
// an unreadable entry is flagged. Shared by the status detail block and gaffer diff.
func (tw *textWriter) writeLedgerProvenance(c comparison) {
	if at := c.lastDeployTime(); !at.IsZero() {
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
func (tw *textWriter) WriteStatusTable(entries []statusEntry) {
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
				return tw.driftStyle(entries[row].comparison).PaddingRight(pad)
			default:
				return tw.styles.emitted.PaddingRight(pad)
			}
		})
	for _, e := range entries {
		t.Row(e.Name, runtimeStateText(e), progressText(e), lastDeployText(e.comparison), deployedViaText(e.comparison), driftVerdict(e.comparison))
	}
	// Trim the column padding the last cell leaves as trailing whitespace (plain
	// in piped output; invisible inside the colour codes on a terminal).
	for line := range strings.SplitSeq(strings.TrimRight(t.String(), "\n"), "\n") {
		tw.write("%s\n", strings.TrimRight(line, " "))
	}
	for _, e := range entries {
		if e.State == driftDrifted {
			tw.write("\n%s\n", tw.styles.pipe.Render("Drifted - run gaffer diff <projection> to see what changed."))
			break
		}
	}
}

func runtimeStateText(e statusEntry) string {
	if e.runtime == nil {
		return "-"
	}
	return string(e.runtime.State)
}

func progressText(e statusEntry) string {
	if e.runtime == nil {
		return "-"
	}
	if e.runtime.Progress < 0 {
		return "unknown"
	}
	return fmt.Sprintf("%.0f%%", e.runtime.Progress)
}

func (tw *textWriter) runtimeStateStyle(e statusEntry) lipgloss.Style {
	if e.runtime == nil {
		return tw.styles.emitted
	}
	switch e.runtime.State {
	case remote.StateRunning:
		return tw.styles.added
	case remote.StateFaulted:
		return tw.styles.errStatus
	default:
		return tw.styles.emitted
	}
}

// lastDeployText / deployedViaText fill the table's provenance columns. The date
// is the last-deploy/write time (from the ledger, else the deployed event), so it
// shows even for a projection with no tool metadata; "via" needs a tool entry. The
// table shows the date alone for scanning; the detail block adds the time.
func lastDeployText(c comparison) string {
	at := c.lastDeployTime()
	if at.IsZero() {
		return "-"
	}
	return at.Format("2006-01-02")
}

func deployedViaText(c comparison) string {
	if c.Ledger == nil {
		return "-"
	}
	return c.Ledger.Tool
}

func driftText(d driftState) string {
	switch d {
	case driftInSync:
		return "in sync"
	case driftDrifted:
		return "drifted"
	case driftNotDeployed:
		return "not deployed"
	case driftUntracked:
		return "untracked"
	case driftInvalid:
		return "invalid"
	default:
		return string(d)
	}
}

// driftStyle colours the verdict by meaning: green healthy, red broken, orange
// wants-attention (drift, and orphan - your abandoned deploy), grey neutral (not
// deployed, and untracked - on the server but not yours). Keys off the comparison,
// since orphan and plain-untracked share the driftUntracked state.
func (tw *textWriter) driftStyle(c comparison) lipgloss.Style {
	switch c.State {
	case driftInSync:
		return tw.styles.added
	case driftInvalid:
		return tw.styles.errStatus
	case driftNotDeployed:
		return tw.styles.muted
	case driftUntracked:
		if c.owner() == ownerOrphan {
			return tw.styles.warning
		}
		return tw.styles.muted
	default: // drifted
		return tw.styles.warning
	}
}
