package drift

// Verdict is the terse comparison verdict shared by gaffer status (table and
// detail), gaffer diff, and the editor status surface. Drift is refined by
// attribution (local ahead / changed externally) and an untracked projection by
// ownership (orphan vs plain untracked - the deployer/provenance names the tool
// behind it).
func Verdict(c Comparison) string {
	switch c.State {
	case Untracked:
		if c.Owner() == OwnerOrphan {
			return "orphan"
		}
		return "untracked"
	case Drifted:
		switch c.Attribution() {
		case AttrLocalAhead:
			return "local ahead"
		case AttrChangedByTool, AttrChangedServer:
			return "changed externally"
		default:
			return "drifted"
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
		return "in sync"
	case Drifted:
		return "drifted"
	case NotDeployed:
		return "not deployed"
	case Untracked:
		return "untracked"
	case Invalid:
		return "invalid"
	default:
		return string(s)
	}
}
