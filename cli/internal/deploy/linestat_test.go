package deploy

import "testing"

func TestLineStat(t *testing.T) {
	for _, tc := range []struct {
		name           string
		remote, local  string
		added, removed int
	}{
		{"identical", "a\nb\nc\n", "a\nb\nc\n", 0, 0},
		{"line-ending noise only", "a\r\nb\r\n", "a\nb\n", 0, 0},
		{"pure additions", "a\n", "a\nb\nc\n", 2, 0},
		{"pure removals", "a\nb\nc\n", "a\n", 0, 2},
		{"replace middle line", "a\nx\nc\n", "a\ny\nc\n", 1, 1},
		{"empty remote", "", "a\nb\n", 2, 0},
		{"empty local", "a\nb\n", "", 0, 2},
		{"both empty", "", "", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			added, removed := LineStat(tc.remote, tc.local)
			if added != tc.added || removed != tc.removed {
				t.Errorf("LineStat = +%d -%d, want +%d -%d", added, removed, tc.added, tc.removed)
			}
		})
	}
}
