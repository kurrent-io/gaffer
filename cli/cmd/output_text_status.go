package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// WriteStatus renders a single projection's status as a detail block: its
// runtime state (when deployed) and how it compares to local.
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
	tw.detail("Drift", tw.driftStyle(e.State).Render(driftBlockText(e.State)))
	if e.State == driftInvalid && e.LocalErr != nil {
		tw.blank()
		tw.write("%s\n", tw.styles.errDetail.Render(e.LocalErr.Error()))
	}
}

// driftBlockText spells out the one-sided verdicts for the single-projection
// block (the table keeps the terse driftText), matching gaffer diff's phrasing.
func driftBlockText(d driftState) string {
	switch d {
	case driftNotDeployed:
		return "not deployed (local only)"
	case driftUntracked:
		return "untracked (deployed, not in gaffer.toml)"
	case driftInvalid:
		return "invalid (local definition)"
	default:
		return driftText(d)
	}
}

// WriteStatusTable renders all projections as a borderless aligned table. The
// cell text is plain; colour is applied per cell by the StyleFunc keying off the
// row's entry, so lipgloss's ANSI-aware width keeps the columns aligned.
func (tw *textWriter) WriteStatusTable(entries []statusEntry) {
	const pad = 3
	t := table.New().
		BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
		BorderColumn(false).BorderRow(false).BorderHeader(false).
		Headers("PROJECTION", "STATE", "PROGRESS", "DRIFT").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return tw.styles.heading.PaddingRight(pad)
			}
			switch col {
			case 1:
				return tw.runtimeStateStyle(entries[row]).PaddingRight(pad)
			case 3:
				return tw.driftStyle(entries[row].State).PaddingRight(pad)
			default:
				return tw.styles.emitted.PaddingRight(pad)
			}
		})
	for _, e := range entries {
		t.Row(e.Name, runtimeStateText(e), progressText(e), driftText(e.State))
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

func (tw *textWriter) driftStyle(d driftState) lipgloss.Style {
	switch d {
	case driftInSync:
		return tw.styles.added
	case driftInvalid:
		return tw.styles.errStatus
	default:
		return tw.styles.warning
	}
}
