package updatecheck

import (
	"fmt"
	"io"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/charmtone"
)

// renderNotice formats the upgrade-available notice as a fang-codeblock-
// style card: padded background fill, indented from the left margin,
// with the available and current versions highlighted using fang's own
// colour roles so the notice sits alongside `gaffer --help` rather than
// clashing with it.
//
// The palette is sourced directly from charmtone (the same vocabulary
// fang uses in its DefaultColorScheme) so when charm bumps the
// underlying values both `gaffer --help` and this notice move together
// rather than drifting silently. Codeblock-dark is a literal "#2F2E36"
// rather than a charmtone Key because fang's own theme.go does the
// same - charmtone has no named Key for that exact tone.
//
// lipgloss.NewRenderer(stderr) detects TTY support from the writer:
// production passes os.Stderr (already TTY-gated by the caller) and
// gets colour; tests pass a *bytes.Buffer and get plain ASCII, so the
// substring assertions in client_test.go keep working unchanged.
func renderNotice(stderr io.Writer, current, latest string) string {
	r := lipgloss.NewRenderer(stderr)

	codeblock := lipgloss.AdaptiveColor{Light: charmtone.Salt.Hex(), Dark: "#2F2E36"}
	base := lipgloss.AdaptiveColor{Light: charmtone.Charcoal.Hex(), Dark: charmtone.Ash.Hex()}
	highlight := lipgloss.AdaptiveColor{Light: charmtone.Pony.Hex(), Dark: charmtone.Cheeky.Hex()}
	dim := lipgloss.AdaptiveColor{Light: charmtone.Squid.Hex(), Dark: charmtone.Oyster.Hex()}
	program := lipgloss.AdaptiveColor{Light: charmtone.Malibu.Hex(), Dark: charmtone.Guppy.Hex()}

	// Background-on-every-segment is fang's own pattern - without it
	// lipgloss loses the background colour between coloured spans on
	// the same line.
	bg := r.NewStyle().Background(codeblock)
	box := bg.
		Foreground(base).
		MarginLeft(2).
		MarginTop(1).
		MarginBottom(1).
		Padding(1, 2)
	available := bg.Foreground(highlight).Bold(true)
	muted := bg.Foreground(dim)
	command := bg.Foreground(program)

	line1 := bg.Render("gaffer ") + available.Render(latest) +
		bg.Render(" available ") + muted.Render("(you have "+current+")")
	line2 := bg.Render("Update with ") +
		command.Render(fmt.Sprintf("npm install -g %s@latest", packageName))

	return box.Render(line1 + "\n" + line2)
}
