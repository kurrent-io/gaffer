package deploy

import (
	"testing"
)

func line(k LineKind, oldN, newN int, text string) DiffLine {
	return DiffLine{Kind: k, OldN: oldN, NewN: newN, Text: text}
}

func assertLines(t *testing.T, got, want []DiffLine) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d\ngot: %+v", len(got), len(want), got)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.Kind != w.Kind || g.OldN != w.OldN || g.NewN != w.NewN || g.Text != w.Text {
			t.Errorf("line %d = {%v %d %d %q}, want {%v %d %d %q}",
				i, g.Kind, g.OldN, g.NewN, g.Text, w.Kind, w.OldN, w.NewN, w.Text)
		}
	}
}

func TestLineDiffEqual(t *testing.T) {
	q := "fromAll()\n.when({})\n"
	assertLines(t, LineDiff(q, q), []DiffLine{
		line(LineEqual, 1, 1, "fromAll()"),
		line(LineEqual, 2, 2, ".when({})"),
	})
}

func TestLineDiffCanonicalisesLikeCompare(t *testing.T) {
	// BOM + CRLF + extra trailing newlines are inert per Compare/Hash, so they
	// must diff as equal too - the diff and the verdict may never disagree.
	remote := "\uFEFFfromAll()\r\n.when({})\r\n\r\n"
	local := "fromAll()\n.when({})\n"
	for _, dl := range LineDiff(remote, local) {
		if dl.Kind != LineEqual {
			t.Fatalf("canonically-equal queries produced a %v line: %+v", dl.Kind, dl)
		}
	}
}

func TestLineDiffChangedLine(t *testing.T) {
	remote := "fromAll()\ncount(1)\ndone()\n"
	local := "fromAll()\ncount(2)\ndone()\n"
	assertLines(t, LineDiff(remote, local), []DiffLine{
		line(LineEqual, 1, 1, "fromAll()"),
		line(LineRemoved, 2, 0, "count(1)"),
		line(LineAdded, 0, 2, "count(2)"),
		line(LineEqual, 3, 3, "done()"),
	})
}

func TestLineDiffInsertion(t *testing.T) {
	remote := "a\nc\n"
	local := "a\nb\nc\n"
	assertLines(t, LineDiff(remote, local), []DiffLine{
		line(LineEqual, 1, 1, "a"),
		line(LineAdded, 0, 2, "b"),
		line(LineEqual, 2, 3, "c"),
	})
}

func TestLineDiffDeletion(t *testing.T) {
	remote := "a\nb\nc\n"
	local := "a\nc\n"
	assertLines(t, LineDiff(remote, local), []DiffLine{
		line(LineEqual, 1, 1, "a"),
		line(LineRemoved, 2, 0, "b"),
		line(LineEqual, 3, 2, "c"),
	})
}

func TestLineDiffAgainstEmpty(t *testing.T) {
	// A canonically empty side is zero lines (matching queryLines/LineStat), not
	// one blank line - no phantom removed row against an empty remote.
	assertLines(t, LineDiff("", "fromAll()\n"), []DiffLine{
		line(LineAdded, 0, 1, "fromAll()"),
	})
	assertLines(t, LineDiff("fromAll()\n", ""), []DiffLine{
		line(LineRemoved, 1, 0, "fromAll()"),
	})
	if got := LineDiff("", ""); len(got) != 0 {
		t.Errorf("empty vs empty = %+v, want no lines", got)
	}
}

func TestLineDiffEmphasis(t *testing.T) {
	remote := "s.count += 1;\n"
	local := "s.count += 2;\n"
	got := LineDiff(remote, local)
	if len(got) != 2 {
		t.Fatalf("got %d lines, want removed+added pair: %+v", len(got), got)
	}
	rem, add := got[0], got[1]
	if rem.Text[rem.EmphFrom:rem.EmphTo] != "1" {
		t.Errorf("removed emphasis = %q, want \"1\"", rem.Text[rem.EmphFrom:rem.EmphTo])
	}
	if add.Text[add.EmphFrom:add.EmphTo] != "2" {
		t.Errorf("added emphasis = %q, want \"2\"", add.Text[add.EmphFrom:add.EmphTo])
	}
}

func TestLineDiffEmphasisPureInsertionWithinLine(t *testing.T) {
	got := LineDiff("ab\n", "aXb\n")
	if len(got) != 2 {
		t.Fatalf("got %d lines: %+v", len(got), got)
	}
	rem, add := got[0], got[1]
	if rem.EmphFrom != 1 || rem.EmphTo != 1 {
		t.Errorf("removed span = [%d,%d), want empty at 1", rem.EmphFrom, rem.EmphTo)
	}
	if add.Text[add.EmphFrom:add.EmphTo] != "X" {
		t.Errorf("added emphasis = %q, want \"X\"", add.Text[add.EmphFrom:add.EmphTo])
	}
}

func TestLineDiffEmphasisUnevenBlock(t *testing.T) {
	// Two removed, one added: the first pair is emphasised, the unpaired second
	// removal keeps an unset span (it reads as wholly removed, which it is).
	remote := "aaa\nbbb\n"
	local := "aXa\n"
	got := LineDiff(remote, local)
	var rems, adds []DiffLine
	for _, dl := range got {
		switch dl.Kind {
		case LineRemoved:
			rems = append(rems, dl)
		case LineAdded:
			adds = append(adds, dl)
		}
	}
	if len(rems) != 2 || len(adds) != 1 {
		t.Fatalf("got %d removed / %d added, want 2/1: %+v", len(rems), len(adds), got)
	}
	if rems[0].Text[rems[0].EmphFrom:rems[0].EmphTo] != "a" {
		t.Errorf("first removed emphasis = %q, want \"a\"", rems[0].Text[rems[0].EmphFrom:rems[0].EmphTo])
	}
	if rems[1].EmphFrom != 0 || rems[1].EmphTo != 0 {
		t.Errorf("unpaired removed span = [%d,%d), want unset", rems[1].EmphFrom, rems[1].EmphTo)
	}
}

func TestLineDiffEmphasisMultibyteRuneBoundary(t *testing.T) {
	// é (C3 A9) and è (C3 A8) share a leading byte; the span must cover the
	// whole rune on each side, never split it.
	got := LineDiff("café\n", "cafè\n")
	if len(got) != 2 {
		t.Fatalf("got %d lines: %+v", len(got), got)
	}
	for _, dl := range got {
		if dl.Text[dl.EmphFrom:dl.EmphTo] != dl.Text[3:] {
			t.Errorf("emphasis = %q, want the whole accented rune %q", dl.Text[dl.EmphFrom:dl.EmphTo], dl.Text[3:])
		}
	}
}

func TestLineDiffEmphasisNotPairedAcrossEqual(t *testing.T) {
	// A removal separated from an addition by an equal line is not a rewrite
	// pair: neither side may borrow an emphasis span from the other.
	remote := "old\nkeep\n"
	local := "keep\nnew\n"
	for _, dl := range LineDiff(remote, local) {
		if dl.Kind != LineEqual && (dl.EmphFrom != 0 || dl.EmphTo != 0) {
			t.Errorf("%v line %q carries span [%d,%d), want unset (no pair across an equal line)",
				dl.Kind, dl.Text, dl.EmphFrom, dl.EmphTo)
		}
	}
}
