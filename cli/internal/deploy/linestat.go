package deploy

import "strings"

// LineStat returns the added and removed line counts of the change from the
// remote (deployed) query to the local one - the +added/-removed of a diffstat.
// It needs only the longest-common-subsequence length: removed lines are remote
// lines absent from the LCS, added lines are local lines absent from it. The full
// line-by-line diff is left to the external viewer; this is just the summary.
//
// Both queries are canonicalised first, so the counts agree with Compare and Hash
// (line-ending and trailing-newline noise is not counted as a change).
func LineStat(remote, local string) (added, removed int) {
	r := queryLines(remote)
	l := queryLines(local)
	common := lcsLen(r, l)
	return len(l) - common, len(r) - common
}

func queryLines(q string) []string {
	q = strings.TrimSuffix(canonicalQuery(q), "\n")
	if q == "" {
		return nil
	}
	return strings.Split(q, "\n")
}

// lcsLen is the length of the longest common subsequence of a and b, by the
// standard dynamic program with two rolling rows. The length is unique even when
// the subsequence itself is not, so the derived counts are deterministic.
func lcsLen(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			switch {
			case a[i-1] == b[j-1]:
				curr[j] = prev[j-1] + 1
			case prev[j] >= curr[j-1]:
				curr[j] = prev[j]
			default:
				curr[j] = curr[j-1]
			}
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}
