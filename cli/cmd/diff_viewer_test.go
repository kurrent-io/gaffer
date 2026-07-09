package cmd

import (
	"bytes"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
)

func TestExternalDiffCommand(t *testing.T) {
	env := func(v string) func(string) string { return func(string) string { return v } }

	t.Run("set", func(t *testing.T) {
		argv, ok := externalDiffCommand(env("delta --paging never"))
		if !ok || !slices.Equal(argv, []string{"delta", "--paging", "never"}) {
			t.Fatalf("argv=%v ok=%v", argv, ok)
		}
	})
	t.Run("unset renders in-process", func(t *testing.T) {
		if _, ok := externalDiffCommand(env("")); ok {
			t.Fatal("want ok=false with no override - the in-process renderer is the default")
		}
	})
	t.Run("blank is unset", func(t *testing.T) {
		if _, ok := externalDiffCommand(env("   ")); ok {
			t.Fatal("want ok=false for a blank override")
		}
	})
}

func TestOpenSourceDiffExitCodes(t *testing.T) {
	// Exit 1 is "files differ" (always true here) and tolerated; anything higher
	// is the viewer reporting real trouble and must surface.
	if err := openSourceDiff([]string{"sh", "-c", "exit 1"}, "p", "deployed", "a\n", "local", "b\n", io.Discard, io.Discard); err != nil {
		t.Errorf("exit 1 should be tolerated: %v", err)
	}
	if err := openSourceDiff([]string{"sh", "-c", "exit 2"}, "p", "deployed", "a\n", "local", "b\n", io.Discard, io.Discard); err == nil {
		t.Error("exit 2 should surface as an error")
	}
	if err := openSourceDiff([]string{"gaffer-no-such-viewer"}, "p", "deployed", "a\n", "local", "b\n", io.Discard, io.Discard); err == nil {
		t.Error("a missing viewer binary should surface as an error")
	}
}

func TestWriteQueryDiff(t *testing.T) {
	// Eleven lines on each side, so the gutters mix one- and two-digit numbers
	// and single digits must right-align. Assertions are on raw lines: the
	// leading alignment is the point.
	remote := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\ncount(1)\n"
	local := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\ncount(2)\n"
	var buf bytes.Buffer
	newTextWriter(&buf, &buf).WriteQueryDiff(deploy.LineDiff(remote, local))
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 12 {
		t.Fatalf("got %d lines, want 12:\n%s", len(lines), buf.String())
	}
	for i, want := range map[int]string{
		0:  " 1  1   a",
		9:  "10 10   j",
		10: "11    - count(1)",
		11: "   11 + count(2)",
	} {
		if lines[i] != want {
			t.Errorf("line %d = %q, want %q", i, lines[i], want)
		}
	}
}

func TestWriteQueryDiffBlankLines(t *testing.T) {
	// A blank line renders its gutter (or bare marker) with no trailing padding.
	remote := "a\n\nb\n"
	local := "a\n\nc\n"
	var buf bytes.Buffer
	newTextWriter(&buf, &buf).WriteQueryDiff(deploy.LineDiff(remote, local))
	for i, l := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if strings.TrimRight(l, " ") != l {
			t.Errorf("line %d %q has trailing spaces", i, l)
		}
	}
	if !strings.Contains(buf.String(), "2 2\n") {
		t.Errorf("blank equal line should render as its bare gutter:\n%s", buf.String())
	}
}

func TestWriteQueryDiffEqualQueries(t *testing.T) {
	// Callers gate on QueryDiffers, but the renderer must still be total: equal
	// queries render as a plain numbered listing, no markers.
	var buf bytes.Buffer
	newTextWriter(&buf, &buf).WriteQueryDiff(deploy.LineDiff("a\nb\n", "a\nb\n"))
	out := buf.String()
	if strings.Contains(out, "+") || strings.Contains(out, "-") {
		t.Errorf("equal queries produced markers:\n%s", out)
	}
	if !strings.Contains(out, "a") || !strings.Contains(out, "b") {
		t.Errorf("missing lines:\n%s", out)
	}
}

// TestDiffLineTextEmphasis exercises the styled path with a colour profile
// forced on (a test buffer resolves to Ascii, where the tints are no-ops and
// the branches are indistinguishable). The line wash and the span wash are
// distinct backgrounds; the span's must appear only for a genuine mid-line
// emphasis.
func TestDiffLineTextEmphasis(t *testing.T) {
	tw := newTextWriter(io.Discard, io.Discard)
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.ANSI256)
	line := r.NewStyle().Background(lipgloss.Color("52"))
	emph := r.NewStyle().Background(lipgloss.Color("88"))
	const lineBg, emphBg = "48;5;52", "48;5;88"

	if out := tw.diffLineText(deploy.DiffLine{Text: "count(2)", EmphFrom: 6, EmphTo: 7}, "+", line, emph); !strings.Contains(out, emphBg) || !strings.Contains(out, lineBg) {
		t.Errorf("paired span should wash the span in the emphasis tint over the line tint: %q", out)
	}
	if out := tw.diffLineText(deploy.DiffLine{Text: "xyz", EmphFrom: 0, EmphTo: 3}, "+", line, emph); strings.Contains(out, emphBg) {
		t.Errorf("whole-line span should keep the line tint only: %q", out)
	}
	if out := tw.diffLineText(deploy.DiffLine{Text: "xyz"}, "-", line, emph); strings.Contains(out, emphBg) {
		t.Errorf("unset span should keep the line tint only: %q", out)
	}
	if out := tw.diffLineText(deploy.DiffLine{Text: ""}, "-", line, emph); strings.Contains(out, emphBg) || strings.Contains(out, "- ") {
		t.Errorf("blank line should be its bare tinted marker: %q", out)
	}
}
