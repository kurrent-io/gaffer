package lsp

import "testing"

// Per-OS sanity for samePath's case-fold: on Linux ext4 the
// comparison is case-sensitive; on Windows NTFS / macOS APFS
// it's case-insensitive. The pathsCaseFold const is
// build-tagged into either paths_case_sensitive.go or
// paths_case_insensitive.go.
func TestSamePath_CaseFoldMatchesHostOS(t *testing.T) {
	got := samePath("/Foo/Bar.js", "/foo/bar.js")
	if got != pathsCaseFold {
		t.Errorf("case-fold mismatch on this OS: samePath returned %v but pathsCaseFold=%v", got, pathsCaseFold)
	}
}
