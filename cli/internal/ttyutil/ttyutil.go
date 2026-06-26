// Package ttyutil holds the one terminal-detection primitive shared
// across the CLI's TTY gates. Callers used to reimplement "is this a
// terminal?" four ways over two mechanisms (mattn/go-isatty and the
// os.ModeCharDevice bit), which could disagree on cygwin/msys ptys and
// forced any portability fix to be made twice. Per-site policy (which
// fds to require, what to do when not a TTY) stays at each call site;
// only the leaf check lives here.
package ttyutil

import (
	"os"

	"github.com/mattn/go-isatty"
)

// IsTerminal reports whether f is backed by an interactive terminal.
// Returns false for a nil file, a pipe, a regular file, or /dev/null.
// Checks both the native terminal test and the cygwin/msys pty test so
// the answer is consistent everywhere; go-isatty's plain IsTerminal
// alone misses cygwin/msys handles.
func IsTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}
