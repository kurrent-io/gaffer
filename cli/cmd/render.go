package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/muesli/termenv"
)

const indentSize = 3

// padCells left-aligns s in a field of the given display-cell width, padding on
// the right with spaces. It measures with lipgloss.Width (terminal cell width)
// rather than fmt's %-*s (which counts runes), so names with full-width or
// combining characters still align.
func padCells(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-lipgloss.Width(s)))
}

type field struct{ label, value string }

type textWriter struct {
	prefixed
	w      io.Writer
	errW   io.Writer
	r      *lipgloss.Renderer
	line   prefixed
	corner prefixed
	styles textStyles
	// Pending event held between WriteEvent and the matching
	// WriteResult / WriteError. Lets WriteResult drop the entire
	// event block silently when the result is "skipped" and
	// showSkipped is off.
	pending *eventInfo
	// showSkipped renders the per-event skip row + a breakdown in
	// the summary. Set true for fixture mode (the user curated
	// the events; a skip is diagnostic - "you forgot a handler",
	// "your partitionBy returned null"), false for live mode
	// (skips are runtime hygiene noise from $all).
	showSkipped bool
	// compileQuirks holds quirk.* diagnostic codes seen at compile time
	// (captured in WriteInfo); runtimeQuirks the distinct codes streamed via
	// OnDiagnostic during the run. The summary lists their union, so it covers
	// every quirk the run surfaced - header or per-event.
	compileQuirks []string
	runtimeQuirks map[string]bool
	// links is true on interactive terminals (the renderer resolves a
	// non-Ascii colour profile), where diagnostic codes are wrapped in OSC 8
	// hyperlinks to their docs anchor. Off for pipes, CI, and tests so output
	// stays plain and copyable.
	links bool
}

// diagnosticsReferenceURL is the generated reference page; each code has a
// matching `#<code>` anchor. Printed once per summary as a plain `See` line,
// and used as the target when codes are hyperlinked on interactive terminals.
const diagnosticsReferenceURL = "https://gaffer.kurrent.io/reference/diagnostics/"

type textStyles struct {
	label     lipgloss.Style
	pipe      lipgloss.Style
	muted     lipgloss.Style
	dim       lipgloss.Style
	faded     lipgloss.Style
	logLabel  lipgloss.Style
	emitted   lipgloss.Style
	processed lipgloss.Style
	added     lipgloss.Style
	skipped   lipgloss.Style
	warning   lipgloss.Style
	errStatus lipgloss.Style
	errDetail lipgloss.Style
	heading   lipgloss.Style
	info      lipgloss.Style

	// Query-diff line tints: a muted background wash over a changed line, a
	// stronger wash on the span that changed within it. The text keeps its own
	// foreground - the background carries the meaning, so the diff reads
	// quietly (delta-style) rather than as blocks of solid colour.
	diffRemoved     lipgloss.Style
	diffRemovedEmph lipgloss.Style
	diffAdded       lipgloss.Style
	diffAddedEmph   lipgloss.Style
}

type prefixed struct {
	tw  *textWriter
	pfx string
}

func newTextWriter(w, errW io.Writer) *textWriter {
	r := lipgloss.NewRenderer(w)
	tw := &textWriter{
		w:     w,
		errW:  errW,
		r:     r,
		links: r.ColorProfile() != termenv.Ascii,
		styles: textStyles{
			label:     r.NewStyle().Foreground(lipgloss.Color("6")),
			pipe:      r.NewStyle().Faint(true).Foreground(lipgloss.Color("6")),
			muted:     r.NewStyle().Foreground(lipgloss.Color("8")),
			dim:       r.NewStyle().Faint(true),
			faded:     r.NewStyle().Faint(true).Foreground(lipgloss.Color("240")),
			logLabel:  r.NewStyle().Foreground(lipgloss.Color("4")),
			emitted:   r.NewStyle(),
			processed: r.NewStyle().Faint(true).Foreground(lipgloss.Color("2")),
			added:     r.NewStyle().Foreground(lipgloss.Color("2")),
			skipped:   r.NewStyle().Foreground(lipgloss.Color("3")),
			warning:   r.NewStyle().Foreground(lipgloss.Color("3")),
			errStatus: r.NewStyle().Foreground(lipgloss.Color("9")),
			errDetail: r.NewStyle().Foreground(lipgloss.Color("1")),
			heading:   r.NewStyle().Bold(true),
			info:      r.NewStyle().Foreground(lipgloss.Color("4")),

			// Hex tints: a quiet wash for the line, a clearly brighter one for the
			// changed span - the 256-palette's dark steps are too coarse to give
			// both. lipgloss degrades them to the nearest 256-colour off truecolor.
			diffRemoved:     r.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#ffe0e0", Dark: "#3a181b"}),
			diffRemovedEmph: r.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#ffafaf", Dark: "#8a2a30"}),
			diffAdded:       r.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#e2f4e2", Dark: "#16301d"}),
			diffAddedEmph:   r.NewStyle().Background(lipgloss.AdaptiveColor{Light: "#a6e2a6", Dark: "#2e7d3c"}),
		},
	}
	tw.prefixed = prefixed{tw: tw, pfx: tw.ind()}
	tw.line = prefixed{tw: tw, pfx: tw.ind("│")}
	tw.corner = prefixed{tw: tw, pfx: tw.styles.pipe.Render("╰") + " "}
	return tw
}

// warmBackground resolves the terminal's light/dark background now, while
// stdin is still ours. The adaptive diff tints trigger the detection lazily on
// first render, via an OSC query the terminal answers on stdin - inside a
// running bubbletea program that reply never arrives (the program owns the tty
// in raw mode), so the first render would stall on the query timeout (~5s)
// and then guess. Call before handing the terminal to bubbletea.
func (tw *textWriter) warmBackground() {
	tw.r.HasDarkBackground()
}

func (tw *textWriter) ind(lead ...string) string {
	if len(lead) == 0 {
		return strings.Repeat(" ", indentSize)
	}
	return tw.styles.pipe.Render(lead[0]) + strings.Repeat(" ", indentSize-1)
}

func (tw *textWriter) write(format string, args ...any) {
	_, _ = fmt.Fprintf(tw.w, format, args...)
}

func (tw *textWriter) heading(text string) {
	tw.write("%s\n", tw.styles.heading.Render(text))
}

func (tw *textWriter) blank() {
	tw.write("\n")
}

// Severity marker glyphs, defined once so the level's mark stays consistent
// across every command rather than each site hardcoding its own. All width-1 so
// they align in a column (avoid the emoji-presentation ℹ and the double-width
// 🛈, which don't). Use the mark* methods for a styled marker; use these raw
// glyphs where a caller builds and styles its own string (a TUI line that
// truncates to a width, say).
const (
	glyphError   = "✗"
	glyphWarning = "⚠"
	glyphInfo    = "ⓘ"
)

// markError / markWarning / markInfo render the severity glyph in its tint (red /
// amber / blue) for prefixing a message line.
func (tw *textWriter) markError() string   { return tw.styles.errStatus.Render(glyphError) }
func (tw *textWriter) markWarning() string { return tw.styles.warning.Render(glyphWarning) }
func (tw *textWriter) markInfo() string    { return tw.styles.info.Render(glyphInfo) }

func (p prefixed) detail(label, value string) {
	p.tw.write("%s%s %s\n", p.pfx, p.tw.styles.label.Render(label+":"), value)
}

func (p prefixed) status(text string) {
	p.tw.write("%s%s\n", p.pfx, text)
}

func (tw *textWriter) lineSub(label string) string {
	return tw.styles.pipe.Render("├") + " " + label
}

func (tw *textWriter) writeNestedFields(fields []field) {
	mid := prefixed{tw: tw, pfx: tw.ind("│") + tw.ind("│")}
	end := prefixed{tw: tw, pfx: tw.ind("│") + tw.ind("╵")}
	for i, f := range fields {
		if i == len(fields)-1 {
			end.detail(f.label, f.value)
		} else {
			mid.detail(f.label, f.value)
		}
	}
}

// diagnosticAnchor is the docs heading slug for a code: github-slugger's
// lowercase, dot-stripped form (quirk.log.multiParam -> quirklogmultiparam).
// It must match the Starlight heading slug so the CLI links to the same anchor
// a reader gets by copying the heading's own anchor link.
func diagnosticAnchor(code string) string {
	return strings.ToLower(strings.ReplaceAll(code, ".", ""))
}

// linkCode wraps a diagnostic code in an OSC 8 hyperlink to its docs anchor on
// interactive terminals; elsewhere it returns the code unchanged.
func (tw *textWriter) linkCode(code string) string {
	if !tw.links {
		return code
	}
	return termenv.Hyperlink(diagnosticsReferenceURL+"#"+diagnosticAnchor(code), code)
}

func (tw *textWriter) writeDiagnostic(d gafferruntime.Diagnostic) {
	header := fmt.Sprintf("[%s] %s", severityLabel(d.Severity), tw.linkCode(d.Code))
	if d.Range != nil {
		header += fmt.Sprintf(" (line %d, col %d)", d.Range.Start.Line, d.Range.Start.Column)
	}
	tw.write("%s\n", tw.severityStyle(d.Severity).Render(header))
	tw.write("%s%s\n\n", tw.ind(), d.Message)
}

// writeStepDiagnostic renders a runtime quirk as a per-event item in the same
// ├ flow as logs and emits - it streams at the point it fires - with the
// styled [severity] code header and its message on a continuation line. No
// source range; runtime quirks are value-dependent, not tied to a location.
func (tw *textWriter) writeStepDiagnostic(d gafferruntime.Diagnostic) {
	header := fmt.Sprintf("[%s] %s", severityLabel(d.Severity), tw.linkCode(d.Code))
	tw.write("%s\n", tw.lineSub(tw.severityStyle(d.Severity).Render(header)))
	tw.write("%s%s\n", tw.ind("│"), d.Message)
}

func severityLabel(s gafferruntime.DiagnosticSeverity) string {
	switch s {
	case gafferruntime.DiagnosticSeverityError:
		return "error"
	case gafferruntime.DiagnosticSeverityWarning:
		return "warning"
	case gafferruntime.DiagnosticSeverityInformation:
		return "info"
	default:
		return "diagnostic"
	}
}

func (tw *textWriter) severityStyle(s gafferruntime.DiagnosticSeverity) lipgloss.Style {
	switch s {
	case gafferruntime.DiagnosticSeverityError:
		return tw.styles.errStatus
	case gafferruntime.DiagnosticSeverityWarning:
		return tw.styles.warning
	default:
		return tw.styles.info
	}
}
