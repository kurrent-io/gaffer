package deploy

import (
	"strings"
	"unicode/utf8"

	"github.com/aymanbagabas/go-udiff"
)

// LineKind classifies one row of a LineDiff.
type LineKind int

const (
	LineEqual LineKind = iota
	LineRemoved
	LineAdded
)

func (k LineKind) String() string {
	switch k {
	case LineRemoved:
		return "removed"
	case LineAdded:
		return "added"
	default:
		return "equal"
	}
}

// DiffLine is one row of an aligned line diff: the line's text, which side(s)
// it belongs to, and its line number on each side.
type DiffLine struct {
	Kind LineKind
	OldN int    // 1-based line number in the remote (deployed) query; 0 for an added line
	NewN int    // 1-based line number in the local query; 0 for a removed line
	Text string // the line, without its trailing newline

	// EmphFrom/EmphTo bound the span that changed within the line (half-open
	// byte offsets into Text, always on rune boundaries). Set on removed/added
	// lines that pair up across a change block; an empty span (from == to)
	// marks the position of a pure insertion or deletion on the other side.
	// The zero span [0,0) is also what an unpaired line carries, so it means
	// "no intraline information", not an insertion at column 0 - a renderer
	// draws nothing for it either way.
	EmphFrom, EmphTo int
}

// LineDiff computes the aligned line diff of the change from the remote
// (deployed) query to the local one: every line of both, equal lines included,
// so a renderer shows the whole query with the changes in place. Both sides
// are canonicalised first, so the diff agrees with Compare and Hash (a
// CRLF-only delta diffs as equal, exactly as it compares in-sync); LineStat
// counts these rows, so the stat agrees by construction.
func LineDiff(remote, local string) []DiffLine {
	before, after := canonicalQuery(remote), canonicalQuery(local)
	// A canonically empty query ("\n") is zero lines to queryLines and LineStat
	// but one empty line to udiff; special-case both empty sides so the diff
	// never renders a phantom blank line that neither side actually has.
	if before == "\n" || after == "\n" {
		return replaceLines(before, after)
	}
	edits := udiff.Lines(before, after)
	if len(edits) == 0 {
		return equalLines(before)
	}
	// Context spanning both sides entirely, so everything lands in one hunk and
	// no equal line is elided - the renderer decides what to show, not the diff.
	ctx := strings.Count(before, "\n") + strings.Count(after, "\n")
	u, err := udiff.ToUnifiedDiff("remote", "local", before, edits, ctx)
	if err != nil {
		// Unreachable: the edits come from udiff.Lines over the same content, so
		// they're consistent by construction. Degrade to a full replacement
		// rather than panic - still a correct diff, just without alignment.
		return replaceLines(before, after)
	}
	var out []DiffLine
	for _, h := range u.Hunks {
		oldN, newN := h.FromLine, h.ToLine
		for _, l := range h.Lines {
			text := strings.TrimSuffix(l.Content, "\n")
			switch l.Kind {
			case udiff.Equal:
				out = append(out, DiffLine{Kind: LineEqual, OldN: oldN, NewN: newN, Text: text})
				oldN++
				newN++
			case udiff.Delete:
				out = append(out, DiffLine{Kind: LineRemoved, OldN: oldN, Text: text})
				oldN++
			case udiff.Insert:
				out = append(out, DiffLine{Kind: LineAdded, NewN: newN, Text: text})
				newN++
			}
		}
	}
	emphasiseChangedPairs(out)
	return out
}

// equalLines renders an unchanged query as all-equal rows, numbered on both
// sides, so LineDiff is total (a caller needn't special-case "no change").
func equalLines(q string) []DiffLine {
	lines := queryLines(q)
	out := make([]DiffLine, len(lines))
	for i, t := range lines {
		out[i] = DiffLine{Kind: LineEqual, OldN: i + 1, NewN: i + 1, Text: t}
	}
	return out
}

// replaceLines is the alignment-free fallback: every remote line removed, every
// local line added.
func replaceLines(before, after string) []DiffLine {
	var out []DiffLine
	for i, t := range queryLines(before) {
		out = append(out, DiffLine{Kind: LineRemoved, OldN: i + 1, Text: t})
	}
	for i, t := range queryLines(after) {
		out = append(out, DiffLine{Kind: LineAdded, NewN: i + 1, Text: t})
	}
	return out
}

// emphasiseChangedPairs pairs each block of removed lines with the added block
// immediately following it - index-wise, the way git pairs a rewritten run -
// and marks the changed span within each pair. Unpaired lines in an uneven
// block (a 3-line removal replaced by 1 line) keep an unset span: they read as
// wholly removed/added, which they are.
func emphasiseChangedPairs(lines []DiffLine) {
	i := 0
	for i < len(lines) {
		if lines[i].Kind != LineRemoved {
			i++
			continue
		}
		remStart := i
		for i < len(lines) && lines[i].Kind == LineRemoved {
			i++
		}
		addStart := i
		for i < len(lines) && lines[i].Kind == LineAdded {
			i++
		}
		for k := range min(i-addStart, addStart-remStart) {
			emphasise(&lines[remStart+k], &lines[addStart+k])
		}
	}
}

// emphasise bounds the changed span of a removed/added pair: the bytes between
// the pair's common prefix and common suffix. The suffix is measured on the
// post-prefix remainders so the two can't overlap.
func emphasise(rem, add *DiffLine) {
	p := commonPrefix(rem.Text, add.Text)
	s := commonSuffix(rem.Text[p:], add.Text[p:])
	rem.EmphFrom, rem.EmphTo = p, len(rem.Text)-s
	add.EmphFrom, add.EmphTo = p, len(add.Text)-s
}

// commonPrefix is the byte length of the longest common prefix of a and b that
// ends on a rune boundary - two strings differing inside a multi-byte rune
// split before it, not through it. In the RuneError case the decoded sizes
// must match and the raw windows must be byte-equal: a valid U+FFFD (3 bytes)
// and a truncated invalid sequence (1 byte) both decode as RuneError, and
// advancing by one side's size past a mismatched window would walk the count
// out of the other string's bounds.
func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) {
		ra, sa := utf8.DecodeRuneInString(a[n:])
		rb, sb := utf8.DecodeRuneInString(b[n:])
		if ra != rb || sa != sb || (ra == utf8.RuneError && a[n:n+sa] != b[n:n+sb]) {
			break
		}
		n += sa
	}
	return n
}

// commonSuffix is the byte length of the longest common suffix of a and b that
// starts on a rune boundary. Same RuneError discipline as commonPrefix.
func commonSuffix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) {
		ra, sa := utf8.DecodeLastRuneInString(a[:len(a)-n])
		rb, sb := utf8.DecodeLastRuneInString(b[:len(b)-n])
		if ra != rb || sa != sb || (ra == utf8.RuneError && a[len(a)-n-sa:len(a)-n] != b[len(b)-n-sb:len(b)-n]) {
			break
		}
		n += sa
	}
	return n
}
