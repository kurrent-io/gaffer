package styledbox

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestNew_RendersPlainASCIIToNonTTYWriter is the contract test
// callers depend on: when the writer isn't a terminal, no ANSI
// escape codes appear in the output. Substring assertions in
// downstream tests rely on this.
func TestNew_RendersPlainASCIIToNonTTYWriter(t *testing.T) {
	buf := &bytes.Buffer{}
	roles := New(buf)
	rendered := roles.Box.Render(
		roles.BG.Render("hello ") +
			roles.Highlight.Render("world"))
	if strings.ContainsRune(rendered, '\x1b') {
		t.Errorf("ANSI escape leaked into non-TTY output:\n%q", rendered)
	}
	if !strings.Contains(rendered, "hello world") {
		t.Errorf("rendered output dropped content: %q", rendered)
	}
}

// TestNew_BoxAddsMarginAndPadding catches accidental drift in the
// shared layout - both callers (updatecheck, telemetry) lean on the
// 2-space left margin and the 1-line vertical padding to look like
// fang's USAGE box.
func TestNew_BoxAddsMarginAndPadding(t *testing.T) {
	buf := &bytes.Buffer{}
	roles := New(buf)
	rendered := roles.Box.Render("x")
	lines := strings.Split(rendered, "\n")
	// MarginTop(1) + Padding top (1) + content (1) + Padding bottom (1) + MarginBottom(1)
	if len(lines) < 5 {
		t.Errorf("expected at least 5 lines from margin+padding+content+padding+margin, got %d:\n%q", len(lines), rendered)
	}
	// MarginLeft(2): every non-empty line should start with at least 2 spaces.
	for i, ln := range lines {
		if ln == "" {
			continue
		}
		if !strings.HasPrefix(ln, "  ") {
			t.Errorf("line %d missing 2-space left margin: %q", i, ln)
		}
	}
}

// TestNew_EveryRoleCarriesCodeblockBackground locks the invariant
// every caller depends on but no usage-site can enforce: spans
// concatenated inside a card must all carry the codeblock
// background, or lipgloss leaves visible gaps between coloured
// regions. The styles encode that discipline; this test prevents a
// future refactor from silently dropping it.
func TestNew_EveryRoleCarriesCodeblockBackground(t *testing.T) {
	roles := New(&bytes.Buffer{})
	want := roles.Box.GetBackground()
	for name, style := range map[string]lipgloss.Style{
		"BG":        roles.BG,
		"Highlight": roles.Highlight,
		"Muted":     roles.Muted,
		"Command":   roles.Command,
	} {
		if got := style.GetBackground(); got != want {
			t.Errorf("%s.GetBackground() = %v, want %v (must match Box for cross-span continuity)", name, got, want)
		}
	}
}
