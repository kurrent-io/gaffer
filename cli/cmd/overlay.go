package cmd

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// placeOverlay composites a rendered view over a background at (x, y): for
// each overlapping row the background line is cut at the overlay's edges with
// ANSI-aware slicing, so escape sequences stay intact and wide runes aren't
// split. SGR state is reset at both seams so background styling can't bleed
// into the overlay or vice versa. Rows of the overlay that fall outside the
// background are dropped - the caller sizes the overlay to fit. A wide rune
// straddling the right seam is kept whole by the cut, growing that row by one
// cell; tolerable for this timeline (wide runes appear only in free text).
func placeOverlay(x, y int, overlay, background string) string {
	fgLines := strings.Split(overlay, "\n")
	bgLines := strings.Split(background, "\n")
	for i, fg := range fgLines {
		row := y + i
		if row < 0 || row >= len(bgLines) {
			continue
		}
		bg := bgLines[row]
		left := ansi.Truncate(bg, x, "")
		if pad := x - ansi.StringWidth(left); pad > 0 {
			left += strings.Repeat(" ", pad)
		}
		right := ansi.TruncateLeft(bg, x+ansi.StringWidth(fg), "")
		bgLines[row] = left + ansi.ResetStyle + fg + ansi.ResetStyle + right
	}
	return strings.Join(bgLines, "\n")
}

// centerOverlay composites the overlay centered on the background, given the
// background's dimensions (the alt screen's width and height).
func centerOverlay(overlay, background string, width, height int) string {
	w, h := ansi.StringWidth(firstLine(overlay)), strings.Count(overlay, "\n")+1
	return placeOverlay(max(0, (width-w)/2), max(0, (height-h)/2), overlay, background)
}

func firstLine(s string) string {
	first, _, _ := strings.Cut(s, "\n")
	return first
}
