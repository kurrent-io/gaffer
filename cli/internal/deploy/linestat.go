package deploy

import "strings"

// LineStat returns the added and removed line counts of the change from the
// remote (deployed) query to the local one - the +added/-removed of a
// diffstat. It counts the rows of LineDiff, so the stat and a rendered diff
// are one source of truth and can never disagree on screen (on a very large
// rewrite the alignment may be slightly non-minimal; the stat follows it).
//
// Both queries are canonicalised (by LineDiff), so the counts agree with
// Compare and Hash - line-ending and trailing-newline noise is not a change.
func LineStat(remote, local string) (added, removed int) {
	for _, dl := range LineDiff(remote, local) {
		switch dl.Kind {
		case LineAdded:
			added++
		case LineRemoved:
			removed++
		}
	}
	return added, removed
}

// queryLines splits a query into its canonical lines: zero lines for a
// canonically empty query, no trailing empty line otherwise.
func queryLines(q string) []string {
	q = strings.TrimSuffix(canonicalQuery(q), "\n")
	if q == "" {
		return nil
	}
	return strings.Split(q, "\n")
}
