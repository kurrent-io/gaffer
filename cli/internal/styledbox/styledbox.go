// Package styledbox builds the fang-codeblock-style cards the CLI
// uses for one-off notices to the user (update available, telemetry
// first-mint disclosure, future "you've been opted out", etc).
//
// Callers get a set of pre-built lipgloss styles (the codeblock fill,
// a few highlight roles) and assemble their own content. The package
// owns the visual vocabulary (palette + layout) so every card looks
// like part of the same product as `gaffer --help`.
//
// The palette is sourced directly from charmtone, matching what
// fang's DefaultColorScheme uses. When fang bumps its scheme both
// `gaffer --help` and these cards move together. Codeblock-dark is a
// literal "#2F2E36" rather than a charmtone Key because fang's own
// theme.go does the same - charmtone has no named Key for that exact
// tone.
//
// lipgloss.NewRenderer(w) detects TTY support from the writer:
// production passes os.Stderr (typically TTY-gated by the caller)
// and gets colour; tests pass a *bytes.Buffer and get plain ASCII,
// so substring assertions in test code keep working unchanged.
package styledbox

import (
	"io"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/charmtone"
)

// Roles is the bundle of pre-built styles a card builder uses. Each
// field is a fully-configured lipgloss.Style; callers Render with
// them and concatenate the results.
//
// The non-Box styles are "attention roles", not semantic ones - each
// caller picks which role to apply to which content based on what it
// wants the reader's eye to land on. Highlight is the loudest,
// Muted is the quietest, Command sits in between. The colours come
// from charmtone (the palette fang uses), so a card built from
// these roles ends up looking like part of the same product as
// `gaffer --help`.
//
// Every non-Box style carries the same codeblock background as Box
// itself. Concatenated spans inside the card need the background on
// every segment or lipgloss leaves a visible gap between coloured
// regions - the styles encode that discipline so callers don't have
// to remember it per-span.
type Roles struct {
	// Box is the outer card style. Apply to the final assembled
	// multi-line content - it carries the codeblock background,
	// margins, and padding.
	Box lipgloss.Style

	// BG carries the codeblock background and the base foreground
	// (Charcoal/Ash). Use it for body text, scope labels, and any
	// other span that isn't being styled with one of the attention
	// roles below.
	BG lipgloss.Style

	// Highlight is the loudest attention role (Pony/Cheeky), bold.
	// Use for the thing the reader is meant to notice - the newer
	// version in an upgrade hint, a runnable command in an opt-out
	// list.
	Highlight lipgloss.Style

	// Muted is the quietest attention role (Squid/Oyster). Use for
	// tail context, parentheticals, and other "available if you
	// want it but not the headline" text.
	Muted lipgloss.Style

	// Command is the mid attention role (Malibu/Guppy). Use for
	// shell-y snippets, env-var names, links - things that are
	// notable without being the headline.
	Command lipgloss.Style
}

// New returns a Roles bundle configured for the renderer attached to
// w. Pass the same writer that will eventually receive the rendered
// card - the renderer uses it to decide whether to emit ANSI escape
// codes.
func New(w io.Writer) Roles {
	r := lipgloss.NewRenderer(w)

	codeblock := lipgloss.AdaptiveColor{Light: charmtone.Salt.Hex(), Dark: "#2F2E36"}
	base := lipgloss.AdaptiveColor{Light: charmtone.Charcoal.Hex(), Dark: charmtone.Ash.Hex()}
	highlight := lipgloss.AdaptiveColor{Light: charmtone.Pony.Hex(), Dark: charmtone.Cheeky.Hex()}
	muted := lipgloss.AdaptiveColor{Light: charmtone.Squid.Hex(), Dark: charmtone.Oyster.Hex()}
	command := lipgloss.AdaptiveColor{Light: charmtone.Malibu.Hex(), Dark: charmtone.Guppy.Hex()}

	bg := r.NewStyle().Background(codeblock)
	return Roles{
		Box: bg.
			Foreground(base).
			MarginLeft(2).
			MarginTop(1).
			MarginBottom(1).
			Padding(1, 2),
		BG:        bg.Foreground(base),
		Highlight: bg.Foreground(highlight).Bold(true),
		Muted:     bg.Foreground(muted),
		Command:   bg.Foreground(command),
	}
}
