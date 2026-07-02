package cmd

import (
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
)

// historyDiffState is what the diff modal can show for a selected entry.
type historyDiffState int

const (
	// diffReady: the entry's change is computable - a baseline content version
	// was found, or the entry is the genuine first version (diff from empty).
	diffReady historyDiffState = iota
	// diffNoChange: a state change (enable/disable/reconfigure/rewrite/reset/
	// delete) - the definition is identical to the baseline by construction.
	diffNoChange
	// diffBaselineUnloaded: the previous content version is on a page that
	// isn't loaded yet, so the diff is unknown (not "created") until it is.
	diffBaselineUnloaded
)

// historyDiff is the change a history entry introduced, ready for the modal:
// the aligned line diff against its previous content version and the scalar
// dimensions that moved with it.
type historyDiff struct {
	state historyDiffState
	sel   historyVersion
	base  *historyVersion   // the previous content version; nil = diff from empty (created)
	cmp   deploy.Comparison // scalar changes vs the baseline; zero when base is nil
	lines []deploy.DiffLine
}

// historyDiffAt computes the diff for entry i of a classified, newest-first
// history: the entry against the nearest older content version, skipping state
// changes (their content is the baseline's by definition). morePages reports
// whether older entries exist beyond the loaded window - without a loaded
// baseline they make the diff unknown rather than a created-from-empty.
func historyDiffAt(versions []historyVersion, i int, morePages bool) historyDiff {
	sel := versions[i]
	if sel.stateChange() || sel.Definition == nil {
		return historyDiff{state: diffNoChange, sel: sel}
	}
	for j := i + 1; j < len(versions); j++ {
		if versions[j].stateChange() || versions[j].Definition == nil {
			continue
		}
		base := versions[j]
		return historyDiff{
			state: diffReady,
			sel:   sel,
			base:  &base,
			cmp:   deploy.Compare(sel.Definition.Descriptor(), base.Definition.Descriptor()),
			lines: deploy.LineDiff(base.Definition.Query, sel.Definition.Query),
		}
	}
	if morePages {
		return historyDiff{state: diffBaselineUnloaded, sel: sel}
	}
	// The whole stream is in view and nothing older carries content: this entry
	// introduced the definition, so the diff is from empty (all added).
	return historyDiff{
		state: diffReady,
		sel:   sel,
		lines: deploy.LineDiff("", sel.Definition.Query),
	}
}
