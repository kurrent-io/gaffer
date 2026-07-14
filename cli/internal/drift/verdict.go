package drift

// Verdict vocabulary, single-sourced here so every surface (gaffer status,
// gaffer diff, the editor status roll-up) spells a verdict the same way and a
// rename lands everywhere at once.
const (
	LabelInSync            = "in sync"
	LabelDrifted           = "drifted"
	LabelNotDeployed       = "not deployed"
	LabelUntracked         = "untracked"
	LabelInvalid           = "invalid"
	LabelOrphan            = "orphan"
	LabelLocalAhead        = "local ahead"
	LabelChangedExternally = "changed externally"
)

// Verdict is the terse comparison verdict shared by gaffer status (table and
// detail), gaffer diff, and the editor status surface. Drift is refined by
// attribution (local ahead / changed externally) and an untracked projection by
// ownership (orphan vs plain untracked - the deployer/provenance names the tool
// behind it).
func Verdict(c Comparison) string {
	switch c.State {
	case Untracked:
		if c.Owner() == OwnerOrphan {
			return LabelOrphan
		}
		return LabelUntracked
	case Drifted:
		switch c.Attribution() {
		case AttrLocalAhead:
			return LabelLocalAhead
		case AttrChangedByTool, AttrChangedServer:
			return LabelChangedExternally
		default:
			return LabelDrifted
		}
	default:
		return StateLabel(c.State)
	}
}

// StateLabel is the human label for a drift State, used where the verdict is the
// plain state (in sync, not deployed, invalid) with no ownership/attribution
// refinement.
func StateLabel(s State) string {
	switch s {
	case InSync:
		return LabelInSync
	case Drifted:
		return LabelDrifted
	case NotDeployed:
		return LabelNotDeployed
	case Untracked:
		return LabelUntracked
	case Invalid:
		return LabelInvalid
	default:
		return string(s)
	}
}
