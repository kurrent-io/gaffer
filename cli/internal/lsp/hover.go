package lsp

import (
	"encoding/base64"
	"fmt"
	"slices"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// projectionAt returns the located, non-diagnostic projection whose header
// spans the given 0-indexed line, or ok=false when the line isn't on a
// projection header. Diagnostic-bearing projections (missing name/entry,
// duplicate) are skipped: they're non-actionable and carry no status, matching
// how emitCodeLenses suppresses their lenses.
func projectionAt(desc config.Description, line int) (config.ProjectionDescription, bool) {
	for _, p := range desc.Projections {
		if p.Diagnostic != nil {
			continue
		}
		// An unlocated header (zero range) clamps to line 0; skip it so a hover
		// on the file's first line doesn't return a phantom status.
		if p.Range == (config.SourceRange{}) {
			continue
		}
		r := rangeToLSP(p.Range)
		if line >= r.Start.Line && line <= r.End.Line {
			return p, true
		}
	}
	return config.ProjectionDescription{}, false
}

// projHealth is a projection's at-a-glance health in one env: in sync, an
// attention state, or faulted/invalid. healthUnknown is the zero value for a
// cell with no known health - its reason (needs sign-in, failed, loading) is
// carried on the cell's Marker instead.
type projHealth int

const (
	healthUnknown projHealth = iota // env unreachable / needs sign-in / not yet fetched
	healthGreen                     // in sync
	healthOrange                    // attention: drifted, local ahead, changed externally, not deployed
	healthRed                       // faulted or invalid
)

// entryHealth classifies one projection's status in one env. Faulted (runtime)
// and invalid (drift) are red; in sync is green; every other in-config drift
// state - drifted, local ahead, changed externally, not deployed - is orange.
// Runtime states other than faulted (stopped, aborted) don't move the dot; they
// read in the state column instead. This mirrors the env-block roll-up, which
// folds faulted in but tracks no other runtime state, so the two surfaces
// agree. Orphan/untracked never reach here: only in-config projections have a
// header to anchor on.
func entryHealth(e drift.StatusEntry) projHealth {
	if e.State == drift.Invalid {
		return healthRed
	}
	if e.Runtime != nil && e.Runtime.State == remote.StateFaulted {
		return healthRed
	}
	if e.State == drift.InSync {
		return healthGreen
	}
	return healthOrange
}

// healthName is the wire string for a known health level, consumed by the
// client to pick a status badge. Kept stable - the extension matches on these.
func healthName(h projHealth) string {
	switch h {
	case healthGreen:
		return "green"
	case healthOrange:
		return "orange"
	case healthRed:
		return "red"
	default:
		return "unknown"
	}
}

// projEnvCell is a projection's status in one env - one entry in the hover
// list and one dot in the badge row.
// Known is false when the env has no usable status (needs sign-in, the fetch
// failed, or it hasn't landed yet), in which case Health is healthUnknown,
// State is empty, and Note carries the reason; otherwise Health/State/Verdict
// describe the deployed projection. Marker is the wire health the badge row
// uses - the known health, or a reason-specific "locked" / "error" / "loading"
// so the client can distinguish why an env has no reading.
type projEnvCell struct {
	Env     string
	Known   bool
	Health  projHealth
	State   string // runtime lifecycle state; empty when not deployed
	Verdict string // drift verdict
	Note    string // reason shown when !Known
	Marker  string // wire health for the badge
}

// envsInFileOrder returns the environments ordered by where their [env.<name>]
// header appears in the file, so the hover rows and the badge dots read
// top-to-bottom in the order the user wrote them - not the by-name order the
// Description carries for the pickers. Environments with no located header
// (quoted keys) sort last, keeping a stable order among themselves.
func envsInFileOrder(envs []config.EnvDescription) []config.EnvDescription {
	out := append([]config.EnvDescription(nil), envs...)
	slices.SortStableFunc(out, func(a, b config.EnvDescription) int {
		la, lb := a.Range.StartLine, b.Range.StartLine
		if (la == 0) != (lb == 0) {
			if la == 0 {
				return 1 // a unlocated -> after b
			}
			return -1
		}
		return la - lb
	})
	return out
}

// projectionEnvCells assembles one cell per configured env for a projection, in
// the order the environments appear in the file. An env that needs sign-in,
// whose fetch failed, or whose status hasn't landed yet yields an unknown cell
// with an explanatory note.
func projectionEnvCells(desc config.Description, projName string, statuses map[string]envStatus) []projEnvCell {
	envs := envsInFileOrder(desc.Environments)
	out := make([]projEnvCell, 0, len(envs))
	for _, env := range envs {
		cell := projEnvCell{Env: env.Name}
		st, ok := statuses[env.Name]
		switch {
		case !ok:
			// Not cached: either a fetch is in flight, or one hasn't been
			// triggered yet. Either way there's nothing to show but "checking".
			cell.Note = "checking…"
			cell.Marker = "loading"
		case st.Unauthenticated:
			cell.Note = "sign-in needed"
			cell.Marker = "locked"
		case st.Err != nil:
			cell.Note = "status unavailable"
			cell.Marker = "error"
		default:
			e, found := findEntry(st.Entries, projName)
			if !found {
				// Fetched cleanly but this projection wasn't in the result. In
				// practice an in-config projection always is; degrade quietly.
				cell.Note = "no status"
				cell.Marker = "error"
				break
			}
			cell.Known = true
			cell.Health = entryHealth(e)
			cell.Marker = healthName(cell.Health)
			if e.Runtime != nil {
				cell.State = string(e.Runtime.State)
			}
			cell.Verdict = drift.Verdict(e.Comparison)
		}
		out = append(out, cell)
	}
	return out
}

// findEntry returns the status entry for name, ok=false when absent.
func findEntry(entries []drift.StatusEntry, name string) (drift.StatusEntry, bool) {
	for i := range entries {
		if entries[i].Name == name {
			return entries[i], true
		}
	}
	return drift.StatusEntry{}, false
}

// projectionHoverMarkdown renders the per-env status for one projection as a
// borderless list, one env per line: a colored health dot, then the env name,
// its drift verdict, and its runtime state - each field its own code span,
// separated by middots. Only fields we have are shown: an unknown env is just
// its name and reason, and a not-deployed projection omits the runtime state.
// Returns "" when the config declares no environments (nothing to show).
//
// Column alignment isn't attempted: a hover renders in a proportional font, and
// inline code spans collapse padding whitespace, so there's no way to line the
// columns up short of a table (heavy) or a <pre> block (needs client-side HTML).
func projectionHoverMarkdown(desc config.Description, proj config.ProjectionDescription, statuses map[string]envStatus) string {
	cells := projectionEnvCells(desc, proj.Name, statuses)
	if len(cells) == 0 {
		return ""
	}
	lines := make([]string, 0, len(cells))
	for _, c := range cells {
		chunks := []string{code(c.Env)}
		if c.Known {
			chunks = append(chunks, code(c.Verdict))
			if c.State != "" {
				chunks = append(chunks, code(c.State))
			}
		} else {
			chunks = append(chunks, code(c.Note))
		}
		lines = append(lines, fmt.Sprintf("![](%s) %s", markerDotURI(c.Marker), strings.Join(chunks, " · ")))
	}
	// Two trailing spaces before each newline is a Markdown hard break, so the
	// list renders one env per line rather than reflowing into a paragraph.
	return strings.Join(lines, "  \n")
}

// code wraps s in a Markdown code span, dropping the backtick and newline that
// would break the span. Env names are identifiers in practice; verdict and
// state come from fixed vocabularies.
func code(s string) string {
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "\n", " ")
	return "`" + s + "`"
}

// markerDotURI is a data-URI SVG of a single status dot for the hover, matching
// the inline badge shapes: filled for a known health, hollow for locked, hollow
// with a slash for error, faint for loading.
func markerDotURI(marker string) string {
	return "data:image/svg+xml;base64," +
		base64.StdEncoding.EncodeToString([]byte(markerDotSVG(marker)))
}

// markerFill is the fill per known health. Kept in step with the VS Code
// client's palette (editors/vscode/src/lsp/status-badges.ts) - the two dot
// renderers are deliberately separate (hover markdown image vs editor
// decoration) but must agree on color, so both are pinned by tests.
var markerFill = map[string]string{
	"green":  "#3fb950",
	"orange": "#d29922",
	"red":    "#f85149",
}

func markerDotSVG(marker string) string {
	const box, cx, cy, r = 12, 6.0, 6.0, 4.0
	const neutral = "#8b949e"
	circle := func(extra string) string {
		return fmt.Sprintf(`<circle cx="%g" cy="%g" r="%g" %s/>`, cx, cy, r, extra)
	}
	ring := circle(fmt.Sprintf(`fill="none" stroke="%s" stroke-width="1.5"`, neutral))
	var body string
	switch {
	case markerFill[marker] != "":
		body = circle(fmt.Sprintf(`fill="%s"`, markerFill[marker]))
	case marker == "loading":
		body = circle(fmt.Sprintf(`fill="%s" opacity="0.4"`, neutral))
	case marker == "error":
		body = ring + fmt.Sprintf(`<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="%s" stroke-width="1.5"/>`, cx-r, cy+r, cx+r, cy-r, neutral)
	default: // locked and any unknown marker
		body = ring
	}
	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d">%s</svg>`, box, box, box, box, body)
}
