//go:build !windows && !darwin

package lsp

// pathsCaseFold is true on filesystems where two paths differing
// only in letter case refer to the same file. False on Linux
// (typical ext4/xfs/btrfs), true on Windows (NTFS) and macOS
// (APFS default).
const pathsCaseFold = false
