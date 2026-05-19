package updatecheck

import (
	"fmt"
	"io"

	"github.com/kurrent-io/gaffer/cli/internal/styledbox"
)

// renderNotice formats the upgrade-available notice as a fang-codeblock-
// style card. The visual vocabulary (palette + layout) lives in
// styledbox so this notice and the telemetry first-mint banner stay
// in lockstep when the look evolves.
func renderNotice(stderr io.Writer, current, latest string) string {
	s := styledbox.New(stderr)
	line1 := s.BG.Render("gaffer ") + s.Highlight.Render(latest) +
		s.BG.Render(" available ") + s.Muted.Render("(you have "+current+")")
	line2 := s.BG.Render("Update with ") +
		s.Command.Render(fmt.Sprintf("npm install -g %s@latest", packageName))
	return s.Box.Render(line1 + "\n" + line2)
}
